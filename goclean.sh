#!/bin/bash
# The script does automatic checking on a Go package and its sub-packages, including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 2. go vet        (http://golang.org/cmd/vet)
# 3. goimports     (https://github.com/bradfitz/goimports)
# 4. ineffassign   (https://github.com/gordonklaus/ineffassign)

# gometalinter (github.com/alecthomas/gometalinter) is used to run each each
# static checker.

set -ex

# Automatic checks
test -z "$(gometalinter --vendor --disable-all \
--enable=gofmt \
--enable=vet \
--enable=goimports \
--enable=ineffassign \
--deadline=10m ./... | tee /dev/stderr)"
