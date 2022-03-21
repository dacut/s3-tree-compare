package s3compare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/sync/semaphore"
)

const defaultConcurrency int64 = 20

type S3APIClient interface {
	s3.HeadObjectAPIClient
	s3.ListObjectsV2APIClient
}

type S3Comparer struct {
	ctx              context.Context
	wg               *sync.WaitGroup
	ignoredHeaders   map[string]bool
	output           io.Writer
	outputFormat     OutputFormat
	outputMutex      sync.Mutex
	firstJSONWritten uint32
	handler1         *asyncS3Handler
	handler2         *asyncS3Handler
}

func NewS3Comparer(ctx context.Context, output io.Writer, outputFormat OutputFormat,
	s3Client1 S3APIClient, s3Client2 S3APIClient, bucket1, bucket2 string) *S3Comparer {
	sem1 := semaphore.NewWeighted(defaultConcurrency)
	var sem2 *semaphore.Weighted

	if bucket1 == bucket2 {
		// Use the same sempahore if we're using the same bucket
		sem2 = sem1
	} else {
		// Otherwise create a unique sempahore.
		sem2 = semaphore.NewWeighted(defaultConcurrency)
	}

	return &S3Comparer{
		ctx:            ctx,
		wg:             &sync.WaitGroup{},
		ignoredHeaders: make(map[string]bool),
		output:         output,
		outputFormat:   outputFormat,
		handler1: &asyncS3Handler{
			ctx:    ctx,
			sem:    sem1,
			s3:     s3Client1,
			bucket: bucket1,
		},
		handler2: &asyncS3Handler{
			ctx:    ctx,
			sem:    sem2,
			s3:     s3Client2,
			bucket: bucket2,
		},
	}
}

func (s3c *S3Comparer) IgnoreHeader(header string) {
	s3c.ignoredHeaders[strings.ToLower(header)] = true
}

func (s3c *S3Comparer) Concurrency(concurrency uint) {
	sem1 := semaphore.NewWeighted(int64(concurrency))
	var sem2 *semaphore.Weighted

	if s3c.handler1.bucket == s3c.handler2.bucket {
		// Use the same sempahore if we're using the same bucket
		sem2 = sem1
	} else {
		// Otherwise create a unique sempahore.
		sem2 = semaphore.NewWeighted(int64(concurrency))
	}

	s3c.handler1.sem = sem1
	s3c.handler2.sem = sem2
}

func (s3c *S3Comparer) ComparePrefixes(prefix1, prefix2 string) {
	s3c.wg.Add(1)

	go s3c.asyncComparePrefixes(prefix1, prefix2)

	s3c.wg.Wait()

	if s3c.outputFormat == OutputFormatJSON {
		// Close the JSON structure.
		s3c.outputMutex.Lock()
		defer s3c.outputMutex.Unlock()

		firstWritten := atomic.SwapUint32(&s3c.firstJSONWritten, 1)
		if firstWritten == 0 {
			// No output; write the open/close brackets.
			_ = s3c.write([]byte("[]\n"))
		} else {
			// Finish the previous diff report with a newline, then write the close bracket.
			_ = s3c.write([]byte("\n]"))
		}
	}
}

func (s3c *S3Comparer) asyncComparePrefixes(prefix1, prefix2 string) {
	defer s3c.wg.Done()

	resultChan1 := make(chan *asyncListPrefixResult)
	resultChan2 := make(chan *asyncListPrefixResult)

	go s3c.handler1.asyncListPrefix(prefix1, resultChan1)
	go s3c.handler2.asyncListPrefix(prefix2, resultChan2)

	var err1 error
	var err2 error
	var subprefixes1 []string
	var subprefixes2 []string
	var keys1 []string
	var keys2 []string

	for resultChan1 != nil || resultChan2 != nil {
		select {
		case result, ok := <-resultChan1:
			if ok {
				if result.Err != nil {
					err1 = result.Err
				} else {
					subprefixes1 = result.Subprefixes
					keys1 = result.Keys
				}
			} else {
				resultChan1 = nil
			}

		case result, ok := <-resultChan2:
			if ok {
				if result.Err != nil {
					err2 = result.Err
				} else {
					subprefixes2 = result.Subprefixes
					keys2 = result.Keys
				}
			} else {
				resultChan2 = nil
			}

		case <-s3c.ctx.Done():
			return
		}
	}

	if err1 != nil {
		fmt.Fprintf(os.Stderr, "Failed to read from s3://%s/%s: %v\n", s3c.handler1.bucket, prefix1, err1)
		return
	}

	if err2 != nil {
		fmt.Fprintf(os.Stderr, "Failed to read from s3://%s/%s: %v\n", s3c.handler2.bucket, prefix2, err2)
		return
	}

	// Examine subprefixes found.
	i := 0
	j := 0
	var subprefixesToCompare [][]string

	for i < len(subprefixes1) && j < len(subprefixes2) {
		switch {
		case subprefixes1[i] == subprefixes1[j]:
			// Subprefix names are equal. Mark them to be compared.
			subprefix1 := fmt.Sprintf("%s%s", prefix1, subprefixes1[i])
			subprefix2 := fmt.Sprintf("%s%s", prefix2, subprefixes2[j])

			subprefixesToCompare = append(subprefixesToCompare, []string{subprefix1, subprefix2})
			i++
			j++
		case subprefixes1[i] < subprefixes2[j]:
			// Missing from bucket2.
			_ = s3c.printMissing(s3c.handler1.bucket, prefix1, subprefixes1[i], FirstObject)
			i++
		default:
			// Missing from bucket1
			_ = s3c.printMissing(s3c.handler2.bucket, prefix2, subprefixes2[i], SecondObject)
			j++
		}
	}

	// Only one or none of the following loops will be executed.

	// Print any subdirectories at the end of subprefixes1 missing from subprefxes2.
	for ; i < len(subprefixes1); i++ {
		_ = s3c.printMissing(s3c.handler1.bucket, prefix1, subprefixes1[i], FirstObject)
	}

	// Print any subdirectories at the end of subprefixes2 missing from subprefixes1.
	for ; j < len(subprefixes2); j++ {
		_ = s3c.printMissing(s3c.handler2.bucket, prefix2, subprefixes2[j], SecondObject)
	}

	// Look at key names.
	i = 0
	j = 0
	var keysToCompare [][]string

	for i < len(keys1) && j < len(keys2) {
		switch {
		case keys1[i] == keys2[j]:
			// Key names are equal. Mark them to be compared
			key1 := fmt.Sprintf("%s%s", prefix1, keys1[i])
			key2 := fmt.Sprintf("%s%s", prefix2, keys2[j])

			keysToCompare = append(keysToCompare, []string{key1, key2})
			i++
			j++

		case keys1[i] < keys2[j]:
			// Missing from bucket2
			_ = s3c.printMissing(s3c.handler1.bucket, prefix1, keys1[i], FirstObject)
			i++

		default:
			// Missing from bucket1
			_ = s3c.printMissing(s3c.handler2.bucket, prefix2, keys2[j], SecondObject)
			j++
		}
	}

	// Only one or none of the following loops will be executed.

	// Print any keys at the end of keys1 missing from keys2
	for ; i < len(keys1); i++ {
		_ = s3c.printMissing(s3c.handler1.bucket, prefix1, keys1[i], FirstObject)
	}

	// Print any keys at the end of keys2 missing from keys1
	for ; j < len(keys2); j++ {
		_ = s3c.printMissing(s3c.handler2.bucket, prefix2, keys2[j], SecondObject)
	}

	// Spawn off goroutines to compare subprefixes
	for _, subprefixes := range subprefixesToCompare {
		s3c.wg.Add(1)

		go s3c.asyncComparePrefixes(subprefixes[0], subprefixes[1])
	}

	// Spawn off goroutines to compare keys
	for _, keys := range keysToCompare {
		s3c.wg.Add(1)

		go s3c.asyncCompareKeys(keys[0], keys[1])
	}
}

func (s3c *S3Comparer) asyncCompareKeys(key1, key2 string) {
	defer s3c.wg.Done()

	rc1 := make(chan *asyncHeadObjectResult, 1)
	rc2 := make(chan *asyncHeadObjectResult, 1)

	go s3c.handler1.asyncHeadObject(key1, rc1)
	go s3c.handler2.asyncHeadObject(key2, rc2)

	var result1 *asyncHeadObjectResult
	var result2 *asyncHeadObjectResult

	for rc1 != nil || rc2 != nil {
		select {
		case result := <-rc1:
			result1 = result

			rc1 = nil

		case result := <-rc2:
			result2 = result
			rc2 = nil

		case <-s3c.ctx.Done():
			return
		}
	}

	if result1.Err != nil {
		fmt.Fprintf(os.Stderr, "HeadObject on s3://%s/%s failed: %v\n", s3c.handler1.bucket, key1, result1.Err)
	}

	if result2.Err != nil {
		fmt.Fprintf(os.Stderr, "HeadObject on s3://%s/%s failed: %v\n", s3c.handler1.bucket, key2, result2.Err)
	}

	if result1.Err != nil || result2.Err != nil {
		return
	}

	headers1 := headObjectOutputToHeaders(result1.Result)
	headers2 := headObjectOutputToHeaders(result2.Result)
	diffsFound := false

	// See if we have any header diffs.
	dr := DiffReport{
		Type: "Mismatch",
		Objects: []DiffObject{
			{
				URL:          fmt.Sprintf("s3://%s/%s", s3c.handler1.bucket, key1),
				LastModified: result1.Result.LastModified.Format(time.RFC3339Nano),
			},
			{
				URL:          fmt.Sprintf("s3://%s/%s", s3c.handler2.bucket, key2),
				LastModified: result2.Result.LastModified.Format(time.RFC3339Nano),
			},
		},
		CommonHeaders: make(map[string]string),
		DiffHeaders:   make(map[string][]string),
	}

	for key, value1 := range headers1 {
		value2, found := headers2[key]

		if found && value1 == value2 {
			dr.CommonHeaders[key] = value1
		} else {
			dr.DiffHeaders[key] = []string{value1, value2}

			// Mark this as a diff only if the header isn't ignored
			if !s3c.ignoredHeaders[key] {
				diffsFound = true
			}
		}

		if found {
			delete(headers2, key)
		}
	}

	for key, value2 := range headers2 {
		// These headers are known to be missing from headers1
		dr.DiffHeaders[key] = []string{"", value2}

		if !s3c.ignoredHeaders[key] {
			diffsFound = true
		}
	}

	if !diffsFound {
		// All non-ignored headers equal; stop here.
		return
	}

	_ = s3c.printDiff(&dr)
}

func (s3c *S3Comparer) printDiff(dr *DiffReport) error {
	if s3c.outputFormat == OutputFormatText {
		return s3c.printDiffText(dr)
	}

	return s3c.printDiffJSON(dr)
}

func (s3c *S3Comparer) write(data []byte) error {
	totalWritten := 0
	for totalWritten < len(data) {
		nWritten, err := s3c.output.Write(data[totalWritten:])
		if err != nil {
			return err
		}

		totalWritten += nWritten
	}

	return nil
}

func (s3c *S3Comparer) printDiffText(dr *DiffReport) error {
	all := &strings.Builder{}
	body := &strings.Builder{}
	nameLen := maxint(len(dr.Objects[0].URL), len(dr.Objects[1].URL))
	fmt.Fprintf(all, "--- %-.*s %s\n", nameLen, dr.Objects[0].URL, dr.Objects[0].LastModified)
	fmt.Fprintf(all, "+++ %-.*s %s\n", nameLen, dr.Objects[1].URL, dr.Objects[1].LastModified)

	keysSorted := make([]string, 0, len(dr.CommonHeaders)+len(dr.DiffHeaders))
	for key := range dr.CommonHeaders {
		keysSorted = append(keysSorted, key)
	}

	for key := range dr.DiffHeaders {
		keysSorted = append(keysSorted, key)
	}

	sort.Strings(keysSorted)

	// Keep track of the total number of lines in each meta-file.
	path1Lines := 0
	path2Lines := 0

	for _, key := range keysSorted {
		value, found := dr.CommonHeaders[key]
		if found {
			fmt.Fprintf(body, " %s: %s\n", key, value)
			path1Lines++
			path2Lines++
		} else {
			values := dr.DiffHeaders[key]
			if values[0] != "" {
				fmt.Fprintf(body, "-%s: %s\n", key, values[0])
				path1Lines++
			}
			if values[1] != "" {
				fmt.Fprintf(body, "+%s: %s\n", key, values[1])
				path2Lines++
			}
		}
	}

	// Write the unified diff patch line information
	fmt.Fprintf(all, "@@ -1,%d +1,%d @@\n", path1Lines, path2Lines)

	// Append body
	all.WriteString(body.String())

	s3c.outputMutex.Lock()
	defer s3c.outputMutex.Unlock()

	return s3c.write([]byte(all.String()))
}

// jsonSeparator returns the next JSON separator to use for writing output.
// This must be called with outputMutex held.
func (s3c *S3Comparer) jsonSeparator() []byte {
	firstWritten := atomic.SwapUint32(&s3c.firstJSONWritten, 1)
	if firstWritten == 0 {
		return []byte("[\n")
	}

	return []byte(",\n")
}

func (s3c *S3Comparer) printDiffJSON(dr *DiffReport) error {
	drBytes, err := json.Marshal(dr)
	if err != nil {
		return err
	}

	s3c.outputMutex.Lock()
	defer s3c.outputMutex.Unlock()
	var data = s3c.jsonSeparator()
	data = append(data, drBytes...)

	return s3c.write(data)
}

func (s3c *S3Comparer) printMissing(bucket, prefix, key string, position DiffObjectPosition) error {
	if s3c.outputFormat == OutputFormatText {
		data := fmt.Sprintf("Only in s3://%s/%s: %s\n", bucket, prefix, key)

		s3c.outputMutex.Lock()
		defer s3c.outputMutex.Unlock()

		return s3c.write([]byte(data))
	}

	dr := MissingDiffReport(fmt.Sprintf("s3://%s/%s%s", bucket, prefix, key), position)
	drBytes, err := json.Marshal(dr)

	if err != nil {
		return err
	}

	s3c.outputMutex.Lock()
	defer s3c.outputMutex.Unlock()
	data := s3c.jsonSeparator()
	data = append(data, drBytes...)

	return s3c.write(data)
}
