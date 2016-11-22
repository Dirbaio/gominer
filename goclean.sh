#!/bin/bash
# The script does automatic checking on a Go package and its sub-packages, including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 2. go vet        (http://golang.org/cmd/vet)
# 3. goimports     (https://github.com/bradfitz/goimports)

# gometalinter (github.com/alecthomas/gometalinter) is used to run each each
# static checker.

set -ex

# Automatic checks
test -z "$(gometalinter --disable-all \
--enable=gofmt \
--enable=vet \
--enable=goimports \
--deadline=45s $(glide novendor) | tee /dev/stderr)"
test -z "$(go fmt $(glide novendor) | tee /dev/stderr)"
test -z "$(go vet $(glide novendor) 2>&1 | tee /dev/stderr)"

