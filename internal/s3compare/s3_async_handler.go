package s3compare

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/sync/semaphore"
)

type asyncS3Handler struct {
	ctx    context.Context
	sem    *semaphore.Weighted
	s3     S3APIClient
	bucket string
}

type asyncListPrefixResult struct {
	Subprefixes []string
	Keys        []string
	Err         error
}

func (s3ah *asyncS3Handler) asyncListPrefix(prefix string, doneChan chan<- *asyncListPrefixResult) {
	defer close(doneChan)

	result := asyncListPrefixResult{
		Subprefixes: make([]string, 0, 20),
		Keys:        make([]string, 0, 20),
	}
	params := &s3.ListObjectsV2Input{
		Bucket:    &s3ah.bucket,
		Delimiter: aws.String("/"),
		Prefix:    &prefix,
	}

	paginator := s3.NewListObjectsV2Paginator(s3ah.s3, params)
	for paginator.HasMorePages() {
		if s3ah.ctx.Err() != nil {
			return
		}

		if err := s3ah.sem.Acquire(s3ah.ctx, 1); err != nil {
			doneChan <- &asyncListPrefixResult{Err: err}
			return
		}

		loo, err := paginator.NextPage(s3ah.ctx)
		s3ah.sem.Release(1)

		if err != nil {
			doneChan <- &asyncListPrefixResult{Err: err}
			return
		}

		for _, commonPrefix := range loo.CommonPrefixes {
			commonPrefixStr := aws.ToString(commonPrefix.Prefix)
			if !strings.HasPrefix(commonPrefixStr, prefix) {
				panic(fmt.Sprintf(
					"Expected ListObjects on s3://%s/%s to have common prefixes beginning with %s; got CommonPrefix %s",
					s3ah.bucket, prefix, prefix, commonPrefixStr))
			}

			result.Subprefixes = append(result.Subprefixes, commonPrefixStr[len(prefix):])
		}

		for i := range loo.Contents {
			obj := &loo.Contents[i]
			key := aws.ToString(obj.Key)

			if !strings.HasPrefix(key, prefix) {
				panic(fmt.Sprintf(
					"Expected ListObjects on s3://%s/%s to have keys beginning with %s; got Key %s",
					s3ah.bucket, prefix, prefix, key))
			}

			result.Keys = append(result.Keys, key[len(prefix):])
		}
	}

	sort.Strings(result.Subprefixes)
	sort.Strings(result.Keys)

	doneChan <- &result
}

type asyncHeadObjectResult struct {
	Result *s3.HeadObjectOutput
	Err    error
}

func (s3ah *asyncS3Handler) asyncHeadObject(key string, resultChan chan<- *asyncHeadObjectResult) {
	defer close(resultChan)

	if err := s3ah.sem.Acquire(s3ah.ctx, 1); err != nil {
		resultChan <- &asyncHeadObjectResult{Err: err}

		return
	}

	hoi := &s3.HeadObjectInput{Bucket: &s3ah.bucket, Key: &key}
	hoo, err := s3ah.s3.HeadObject(s3ah.ctx, hoi)
	s3ah.sem.Release(1)
	resultChan <- &asyncHeadObjectResult{Result: hoo, Err: err}
}
