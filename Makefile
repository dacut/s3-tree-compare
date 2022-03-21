TAG_VERSION := $(shell git tag --points-at)
COMMIT_VERSION := $(shell git rev-parse --short=10 HEAD)
IS_MODIFIED := $(shell git status --short --porcelain)
USERNAME := $(if $(LOGNAME),$(LOGNAME),$(if $(USER),$(USER),$(shell whoami)))
VERSION := $(if $(IS_MODIFIED),$(COMMIT_VERSION)-$(USERNAME),$(if $(TAG_VERSION),$(TAG_VERSION),$(COMMIT_VERSION)))
ARTIFACTORY_REPOSITORY = $(if $(IS_MODIFIED),general-develop,$(if $(TAG_VERSION),general,general-stage))
PLATFORMS := macos-universal linux-aarch64 linux-x86_64 windows-aarch64 windows-x86_64
ZIP_TARGETS := $(foreach platform,$(PLATFORMS),s3-tree-compare-$(platform)-$(VERSION).zip)
EXE_TARGETS := $(foreach platform,$(PLATFORMS),s3-tree-compare-$(platform))
UPLOAD_TARGETS := $(foreach platform,$(PLATFORMS),upload-$(platform))

all: $(ZIP_TARGETS) version.go

test: version.go
	go test

upload: $(UPLOAD_TARGETS)

upload-%: s3-tree-compare-%-$(VERSION).zip
	./artifactory-upload $(ARTIFACTORY_REPOSITORY) $<

s3-tree-compare-%-$(VERSION).zip: go.mod go.sum *.go version.go
	./build $@

clean:
	rm -rf s3-tree-compare-* s3-tree-compare tmp-* version.go

version.go:
	go generate

.PHONY: all clean