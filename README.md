# S3 Tree Compare
Compare two hierarchies in S3 without downloading files (objects).

This calls HeadObject on each object across two S3 paths, reporting any differences found. Two objects are considered
different if:

* One object is missing.
* The content lengths do not match.
* HTTP headers do not match (unless ignored via `ignore-header`):
  * `ETag`
  * `Cache-Control`
  * `Content-Dispostion`
  * `Content-Encoding`
  * `Content-Language`
  * `Content-Type`
  * `Expires`
  * `Website-Redirect-Location`
  * `x-amz-meta-*` headers

## Usage

`s3-tree-compare [options] <s3://bucket1/prefix1/> <s3://bucket2/prefix2>`

### Options

Single or double-dashes may be used to specify options.

Global options:

* `-concurrency=<int>` — The maximum number of S3 calls in-flight (per-bucket). Defaults to 20.
* `-format=<json|text>` — Output format. Defaults to `text`.
* `-ignore-header=<header-name>` — Ignore the specified header. Can be specified multiple times.
* `-output=<filename>` — Write output to the specified file. Defaults to stdout.

S3 options can be specified globally (e.g. `-region`) or per-path (`-region1`/`-region2`):
* `-endpoint=<url>` (`-endpoint1`/`-endpoint2`) — S3 endpoint to use (for non-AWS systems). You
  must specify the region in this case (for the request to be signed properly).
* `-profile=<name>` (`-profile1`/`-profile2`) — Read static AWS credentials from the specified profile.
* `-region=<name>` (`-region1`/`-region2`) — Specify the AWS region to use for making calls to S3.

AWS setup is also influenced by the following environment variables:

* `AWS_PROFILE`/`AWS_PROFILE1`/`AWS_PROFILE2` — Equivalent to `-profile`/`-profile1`/`-profile2`.
* `AWS_REGION`/`AWS_REGION1`/`AWS_REGION2` — Equivalent to `-region`/`-region1`/`-region2`.
* `AWS_ACCESS_KEY`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN` — Credentials to use for S3.
* `AWS_ACCESS_KEY1`, `AWS_SECRET_ACCESS_KEY1`, `AWS_SESSION_TOKEN1` — Credentials to use for first S3 path.
* `AWS_ACCESS_KEY2`, `AWS_SECRET_ACCESS_KEY2`, `AWS_SESSION_TOKEN2` — Credentials to use for second S3 path.

## Output

Text output is in unified diff format:
```
--- s3://bucket-a/user1/build.yml 2021-12-16T16:59:39Z
+++ s3://bucket-b/test-projects/user2/build.yml 2022-03-16T04:58:49Z
@@ -1,6 +1,4 @@
 content-length: 3809
 content-type: binary/octet-stream
 etag: "6c39f6182d20cd6043e3767a3fc58663"
-x-amz-meta-file-group: 10034
-x-amz-meta-file-owner: 56519
-x-amz-meta-file-permissions: 0644
+x-amz-meta-file-permissions: 0755
--- s3://bucket-a/user1/README.md 2021-05-26T20:56:05Z
+++ s3://bucket-b/test-projects/user2/README.md 2022-03-16T04:58:49Z
@@ -1,6 +1,4 @@
 content-length: 151
 content-type: text/plain
 etag: "c3f40ced91df23bff8deb579bb730b5a"
-x-amz-meta-file-group: 20
-x-amz-meta-file-owner: 503
-x-amz-meta-file-permissions: 0644
+x-amz-meta-file-permissions: 0755
--- s3://bucket-a/user1/Makefile 2021-12-16T16:59:39Z
+++ s3://bucket-b/test-projects/user2/Makefile 2022-03-16T04:58:49Z
@@ -1,6 +1,4 @@
 content-length: 941
 content-type: binary/octet-stream
 etag: "99d87b0f49a0474dd70a8d921270f7e7"
-x-amz-meta-file-group: 10034
-x-amz-meta-file-owner: 56519
-x-amz-meta-file-permissions: 0644
+x-amz-meta-file-permissions: 0755
Only in s3://bucket-a/user1/: build-output/
```

JSON output is in a custom format:
```json
[
    {
        "Type": "Mismatch"|"Missing",
        "DiffObjects": [ # One of these will be omitted if Type is "Missing"
            {
                "Url": "s3://<bucket>/<key>",
                "LastModified": "YYYY-MM-DDTHH:MM:SSZ"
            },
            {
                "Url": "s3://<bucket>/<key>",
                "LastModified": "YYYY-MM-DDTHH:MM:SSZ"
            }
        ],
        "CommonHeaders": {
            "key": "value"
        },
        "DiffHeaders": {
            "key": "value"
        }
    },
    ...
]
```

Each JSON diff object is on a single line, suitable for use with `grep` if needed.

For example:
```json
[
{"Type":"Mismatch","DiffObjects":[{"Url":"s3://bucket-a/user1/build.yml","LastModified":"2021-12-16T16:59:39Z"},{"Url":"s3://bucket-b/test-projects/user2/build.yml","LastModified":"2022-03-16T04:58:49Z"}],"CommonHeaders":{"content-length":"3809","content-type":"binary/octet-stream","etag":"\"6c39f6182d20cd6043e3767a3fc58663\""},"DiffHeaders":{"x-amz-meta-file-group":["10034",""],"x-amz-meta-file-owner":["56519",""],"x-amz-meta-file-permissions":["0644","0755"]}},
{"Type":"Mismatch","DiffObjects":[{"Url":"s3://bucket-a/user1/README.md","LastModified":"2021-05-26T20:56:05Z"},{"Url":"s3://bucket-b/test-projects/user2/README.md","LastModified":"2022-03-16T04:58:49Z"}],"CommonHeaders":{"content-length":"151","content-type":"text/plain","etag":"\"c3f40ced91df23bff8deb579bb730b5a\""},"DiffHeaders":{"x-amz-meta-file-group":["20",""],"x-amz-meta-file-owner":["503",""],"x-amz-meta-file-permissions":["0644","0755"]}},
{"Type":"Mismatch","DiffObjects":[{"Url":"s3://bucket-a/user1/Makefile","LastModified":"2021-12-16T16:59:39Z"},{"Url":"s3://test-projects/user2/Makefile","LastModified":"2022-03-16T04:58:49Z"}],"CommonHeaders":{"content-length":"941","content-type":"binary/octet-stream","etag":"\"99d87b0f49a0474dd70a8d921270f7e7\""},"DiffHeaders":{"x-amz-meta-file-group":["10034",""],"x-amz-meta-file-owner":["56519",""],"x-amz-meta-file-permissions":["0644","0755"]}},
{"Type":"Missing","DiffObjects":[{"Url":"s3://bucket-a/user1/build-output/"},{"Url":""}]}
]
```