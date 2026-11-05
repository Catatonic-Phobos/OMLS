#!/usr/bin/env bash
set -euo pipefail
omls_root="${1:?usage: build.sh OMLS_ROOT}"
gfxstream_source="$omls_root/sharing/development/gfxstream"
gfxstream_build="$omls_root/sharing/development/gfxstream-build"
output="$omls_root/sharing/runtime/omls-gfxstream-host"
mkdir -p "$(dirname "$output")"
c++ -std=c++17 -O2 -pthread \
    -I"$gfxstream_source/host/include" \
    -I"$gfxstream_source/host/features/include" \
    "$omls_root/render/gfxstream-host/main.cpp" \
    "$omls_root/render/gfxstream-host/mux.cpp" \
    -L"$gfxstream_build/host" -Wl,-rpath,"$gfxstream_build/host" \
    -lgfxstream_backend -o "$output.new"
chmod 700 "$output.new"
mv "$output.new" "$output"
