package main

import (
	"errors"
	"strings"
)

const s3URLPrefix = "s3://"

func parseS3URL(s3URLString string) (bucket string, key string, err error) {
	if !strings.HasPrefix(s3URLString, s3URLPrefix) {
		err = errors.New(`S3 URL must begin with ` + s3URLPrefix) //nolint:stylecheck // proper noun
	} else {
		s3URLString = s3URLString[len(s3URLPrefix):]
		parts := strings.SplitN(s3URLString, "/", 2)
		bucket = parts[0]

		if len(parts) > 1 {
			key = parts[1]
		}
	}

	return
}
