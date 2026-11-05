#!/usr/bin/env bash
set -euo pipefail
omls_root="${1:?usage: setup.sh OMLS_ROOT}"
mesa_source="$omls_root/sharing/development/mesa"
mesa_build="$omls_root/sharing/development/mesa-build"
expected_revision=38377311965f94287e6b90f6b264861582cb5428
actual_revision="$(git -C "$mesa_source" rev-parse HEAD)"
if [[ "$actual_revision" != "$expected_revision" ]]; then
    echo 'Mesa source revision does not match the OMLS guest patch.' >&2
    exit 1
fi
patch_file="$omls_root/render/mesa-guest/mesa.patch"
if ! git -C "$mesa_source" apply --reverse --check "$patch_file" 2>/dev/null; then
    git -C "$mesa_source" apply --check "$patch_file"
    git -C "$mesa_source" apply "$patch_file"
fi
export PYTHONPATH="$omls_root/sharing/development/python-packages${PYTHONPATH:+:$PYTHONPATH}"
export PKG_CONFIG_PATH="$omls_root/sharing/development/deps/usr/lib/x86_64-linux-gnu/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
extra_options=()
if [[ -f "$mesa_build/build.ninja" ]]; then extra_options+=(--reconfigure); fi
python3 -m mesonbuild.mesonmain setup "${extra_options[@]}" "$mesa_build" "$mesa_source" \
    -Dvulkan-drivers=gfxstream -Dgallium-drivers=[] -Dplatforms=[] \
    -Dopengl=false -Dglx=disabled -Degl=disabled -Dgbm=disabled -Dllvm=disabled \
    -Dvideo-codecs=[] -Dvalgrind=disabled -Dlibunwind=disabled -Dzstd=disabled \
    -Dbuildtype=release
nice -n 15 ninja -C "$mesa_build" -j1
