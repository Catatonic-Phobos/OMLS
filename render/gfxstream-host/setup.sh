#!/usr/bin/env bash
set -euo pipefail
omls_root="${1:?usage: setup.sh OMLS_ROOT}"
source_dir="$omls_root/sharing/development/gfxstream"
build_dir="$omls_root/sharing/development/gfxstream-build"
export PYTHONPATH="$omls_root/sharing/development/tools${PYTHONPATH:+:$PYTHONPATH}"
include_flags="-I$source_dir/third_party/xcb/include -I$source_dir/third_party/x11/include -I$source_dir/third_party/opengl/include -I$source_dir/third_party/drm/include"
if [[ -f "$build_dir/build.ninja" ]]; then
    python3 -m mesonbuild.mesonmain configure "$build_dir" "-Dcpp_args=$include_flags" -Ddecoders=gles,vulkan,composer
else
    python3 -m mesonbuild.mesonmain setup "$build_dir" "$source_dir" \
        -Ddefault_library=shared -Dgfxstream-build=host -Ddecoders=gles,vulkan,composer \
        -Dbuildtype=release "-Dcpp_args=$include_flags"
fi
nice -n 10 ninja -C "$build_dir" -j2
bash "$omls_root/render/gfxstream-host/build.sh" "$omls_root"
