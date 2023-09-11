#!/usr/bin/env bash
#
# Copyright (c) 2020-2023 The Decred developers
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
#
# Usage:
#   ./run_tests.sh

set -e

go version

# Run tests.
go test -tags opencl -v ./...

# Run linters.
golangci-lint run

echo "-----------------------------"
echo "Tests completed successfully!"
