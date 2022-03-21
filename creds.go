package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// getLoadOptions returns AWS config load options using the specified set of suffixes appended to environment variables
// and flags. Values found from environment/flags in later suffixes override earlier suffixes; flags override environment
// variables.
func getLoadOptions(flagSet *flag.FlagSet, suffixes []string) ([]func(*config.LoadOptions) error, error) {
	var loadOptions []func(*config.LoadOptions) error
	var profileLoadOption func(*config.LoadOptions) error
	var regionLoadOption func(*config.LoadOptions) error
	var staticProviderLoadOption func(*config.LoadOptions) error
	var endpointLoadOption func(*config.LoadOptions) error

	for _, suffix := range suffixes {
		profile := os.Getenv(fmt.Sprintf("AWS_PROFILE%s", suffix))
		if profile != "" {
			profileLoadOption = config.WithSharedConfigProfile(profile)
		}

		region := os.Getenv(fmt.Sprintf("AWS_REGION%s", suffix))

		if region == "" {
			region = os.Getenv(fmt.Sprintf("AWS_DEFAULT_REGION%s", suffix))
		}

		if region != "" {
			regionLoadOption = config.WithRegion(region)
		}

		accessKey := os.Getenv(fmt.Sprintf("AWS_ACCESS_KEY%s", suffix))
		secretKey := os.Getenv(fmt.Sprintf("AWS_SECRET_ACCESS_KEY%s", suffix))
		token := os.Getenv(fmt.Sprintf("AWS_SESSION_TOKEN%s", suffix))

		if accessKey != "" && secretKey != "" {
			staticProvider := credentials.NewStaticCredentialsProvider(accessKey, secretKey, token)
			staticProviderLoadOption = config.WithCredentialsProvider(staticProvider)
		}
	}

	for _, suffix := range suffixes {
		var region, endpoint string

		flagSet.Visit(func(f *flag.Flag) {
			switch {
			case f.Name == fmt.Sprintf("profile%s", suffix):
				profileLoadOption = config.WithSharedConfigProfile(f.Value.String())
				// Wipe out any environment-provided static credentials so the flag overrides it.
				staticProviderLoadOption = nil
			case f.Name == fmt.Sprintf("region%s", suffix):
				region = f.Value.String()
				regionLoadOption = config.WithRegion(region)
			case f.Name == fmt.Sprintf("endpoint%s", suffix):
				endpoint = f.Value.String()
			}
		})

		if endpoint != "" {
			if region == "" {
				return nil, fmt.Errorf("region must be specified if endpoint is specified")
			}

			endpointResolverFunc := func(service, requestedRegion string, options ...interface{}) (aws.Endpoint, error) {
				if service != "s3" {
					return aws.Endpoint{}, &aws.EndpointNotFoundError{Err: fmt.Errorf("unsupported service %s", service)}
				}

				return aws.Endpoint{
					URL:               endpoint,
					HostnameImmutable: true,
					PartitionID:       "aws",
					SigningName:       "s3",
					SigningRegion:     region, // ignore requestedRegion
					SigningMethod:     "s3v4",
				}, nil
			}
			endpointResolver := aws.EndpointResolverWithOptionsFunc(endpointResolverFunc)
			endpointLoadOption = config.WithEndpointResolverWithOptions(endpointResolver)
		}
	}

	// Load the profile before static credentials so that static credentials override the profile.
	if profileLoadOption != nil {
		loadOptions = append(loadOptions, profileLoadOption)
	}

	if staticProviderLoadOption != nil {
		loadOptions = append(loadOptions, staticProviderLoadOption)
	}

	if regionLoadOption != nil {
		loadOptions = append(loadOptions, regionLoadOption)
	}

	if endpointLoadOption != nil {
		loadOptions = append(loadOptions, endpointLoadOption)
	}

	return loadOptions, nil
}
