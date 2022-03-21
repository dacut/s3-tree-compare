package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/dacut/s3-tree-compare/internal/s3compare"
)

func main() {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	usage := func(w io.Writer) {
		flags.SetOutput(w)
		fmt.Fprintf(w, `Usage: %s [options] s3://bucket1/path1/ s3://bucket2/path2/
Compare two S3 paths for differences by examining metadata (without downloading
objects).

This calls HeadObject on each object found. Any differences found are noted.

Two objects are considered different if:
    - One object is missing.
	- The content lengths do not match.
	- HTTP headers do not match (unless ignored via ignore-header):
        ETag
		Cache-Control
		Content-Disposition
		Content-Encoding
		Content-Language
		Content-Type
		Expires
		Website-Redirect-Location
		x-amz-meta-* headers
`, os.Args[0])

		flags.PrintDefaults()
	}

	ignoredHeadersFlag := &StringListFlag{}

	// Define flags for setting regions, profiles, endpoints. These are handled by getLoadOptions.
	flags.String("endpoint", "", "S3 endpoint to use for both buckets.")
	flags.String("endpoint1", "", "Override S3 endpoint for first S3 bucket.")
	flags.String("endpoint2", "", "Override S3 endpoint for second S3 bucket.")
	flags.String("profile", "", "AWS credential profile to use for both buckets.")
	flags.String("profile1", "", "Override AWS credential profile for first S3 bucket.")
	flags.String("profile2", "", "Override AWS credential profile for second S3 bucket.")
	flags.String("region", "", "Region for both S3 buckets.")
	flags.String("region1", "", "Override region for first S3 bucket.")
	flags.String("region2", "", "Override region for second S3 bucket.")

	concurrency := flags.Int("concurrency", 0, "Maximum concurrent S3 calls in-flight.")
	flags.Var(ignoredHeadersFlag, "ignore-header", "Add header to list of headers to ignore.")
	outputFormatStr := flags.String("format", "text", "Output format (text/json; defaults to text).")
	outputFileFlag := flags.String("output", "", "Write output to specified file (defaults to stdout).")

	help := flags.Bool("help", false, "Show this usage information.")
	err := flags.Parse(os.Args[1:])

	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		usage(os.Stderr)
		os.Exit(1)
	}

	if *help {
		usage(os.Stdout)
		os.Exit(0)
	}

	var output io.Writer
	var outputFormat s3compare.OutputFormat

	switch *outputFormatStr {
	case "text":
		outputFormat = s3compare.OutputFormatText
	case "json":
		outputFormat = s3compare.OutputFormatJSON
	default:
		fmt.Fprintf(os.Stderr, "Invalid value for -output-format: must be text or json: %#v\n", outputFormatStr)
		usage(os.Stderr)
		os.Exit(1)
	}

	if *concurrency < 0 {
		fmt.Fprintf(os.Stderr, "Invalid value for -concurrency: must be greater than 0: %d", *concurrency)
		usage(os.Stderr)
		os.Exit(1)
	}

	locations := flags.Args()
	if len(locations) < 2 {
		fmt.Fprintf(os.Stderr, "Expected two S3 locations to compare\n")
		usage(os.Stderr)
		os.Exit(1)
	}

	bucket1, prefix1, err := parseS3URL(locations[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid S3 URL: %v: %s\n", err, locations[0])
		os.Exit(1)
	}

	bucket2, prefix2, err := parseS3URL(locations[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid S3 URL: %v: %s\n", err, locations[1])
		os.Exit(1)
	}

	loadOptions1, err := getLoadOptions(flags, []string{"", "1"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
		os.Exit(1)
	}

	loadOptions2, err := getLoadOptions(flags, []string{"", "2"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
		os.Exit(1)
	}

	// Cancel all work if we're interrupted.
	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGPIPE, syscall.SIGINT, syscall.SIGTERM)

	awsConfig1, err := config.LoadDefaultConfig(ctx, loadOptions1...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to configure AWS client for %s: %v\n", locations[0], err)
		os.Exit(1)
	}

	awsConfig2, err := config.LoadDefaultConfig(ctx, loadOptions2...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to configure AWS client for %s: %v\n", locations[1], err)
		os.Exit(1)
	}

	// Open up the output (if necessary)
	switch *outputFileFlag {
	case "", "-":
		output = os.Stdout
	default:
		var outputFile *os.File
		outputFile, err = os.Create(*outputFileFlag)

		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to open %s for writing: %v\n", *outputFileFlag, err)
			os.Exit(1)
		}
		defer outputFile.Close()
		output = outputFile
	}

	s3Client1 := s3.NewFromConfig(awsConfig1)
	s3Client2 := s3.NewFromConfig(awsConfig2)

	// Create the comparer and set options
	comparer := s3compare.NewS3Comparer(ctx, output, outputFormat, s3Client1, s3Client2, bucket1, bucket2)

	for _, ignoredHeader := range ignoredHeadersFlag.Values {
		comparer.IgnoreHeader(ignoredHeader)
	}

	if *concurrency > 0 {
		comparer.Concurrency(uint(*concurrency))
	}

	// Run the comparer
	comparer.ComparePrefixes(prefix1, prefix2)
}
