#!/bin/bash
# The script does automatic checking on a Go package and its sub-packages, including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 2. go vet        (http://golang.org/cmd/vet)
# 4. ineffassign   (https://github.com/gordonklaus/ineffassign)

set -ex

# golangci-lint (github.com/golangci/golangci-lint) is used to run each each
# static checker.

# check linters
golangci-lint run --build-tags opencl --disable-all --deadline=10m \
  --enable=gofmt \
  --enable=ineffassign
