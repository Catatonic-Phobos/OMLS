#!/usr/bin/env bash
# Generate Go stubs from proto/omls/v1.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$(go env GOPATH)/bin:${PATH}"

cd "$ROOT"
protoc \
  --go_out=. --go_opt=module=github.com/Catatonic-Phobos/OMLS \
  --go-grpc_out=. --go-grpc_opt=module=github.com/Catatonic-Phobos/OMLS \
  proto/omls/v1/fabric.proto

echo "generated proto/omls/v1"
