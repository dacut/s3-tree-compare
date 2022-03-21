package s3compare

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func headObjectOutputToHeaders(hoo *s3.HeadObjectOutput) map[string]string {
	headers := make(map[string]string)

	headers["content-length"] = fmt.Sprintf("%d", hoo.ContentLength)

	if cacheControl := aws.ToString(hoo.CacheControl); cacheControl != "" {
		headers["cache-control"] = cacheControl
	}

	if contentDisposition := aws.ToString(hoo.ContentDisposition); contentDisposition != "" {
		headers["content-disposition"] = contentDisposition
	}

	if contentEncoding := aws.ToString(hoo.ContentEncoding); contentEncoding != "" {
		headers["content-encoding"] = contentEncoding
	}

	if contentLanguage := aws.ToString(hoo.ContentLanguage); contentLanguage != "" {
		headers["content-language"] = contentLanguage
	}

	if contentType := aws.ToString(hoo.ContentType); contentType != "" {
		headers["content-type"] = contentType
	}

	if etag := aws.ToString(hoo.ETag); etag != "" {
		headers["etag"] = etag
	}

	if hoo.Metadata != nil {
		for key, value := range hoo.Metadata {
			headers[strings.ToLower(fmt.Sprintf("x-amz-meta-%s", key))] = value
		}
	}

	return headers
}

func maxint(a int, b int) int {
	if a < b {
		return b
	}

	return a
}
