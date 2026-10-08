#!/usr/bin/env bash
# Build a downloadable OMLS pack for GitHub Releases (linux/amd64).
# Output: dist/omls-linux-amd64.tar.gz
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
arch="${OMLS_PACK_ARCH:-amd64}"
goos="${OMLS_PACK_OS:-linux}"
name="omls-${goos}-${arch}"
dist_dir="$repo_root/dist"
stage="$dist_dir/$name"

mkdir -p "$stage"
trap 'rm -rf "$stage"' EXIT

cd "$repo_root"
echo "building ${goos}/${arch}…"
CGO_ENABLED=0 GOOS="$goos" GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "$stage/omls" ./cmd/omls

cp "$repo_root/scripts/systemd/omls.service.in" "$stage/omls.service.in"
cp "$repo_root/scripts/pack/install.sh" "$stage/install.sh"
chmod 0755 "$stage/omls" "$stage/install.sh"

# Record pack metadata for omls update.
version="$("$stage/omls" version 2>/dev/null | awk '{print $2}')"
if [[ -z "${version:-}" ]]; then
  version="unknown"
fi
cat >"$stage/PACK.txt" <<EOF
name=$name
version=$version
os=$goos
arch=$arch
repo=Catatonic-Phobos/OMLS
EOF

out="$dist_dir/${name}.tar.gz"
rm -f "$out"
tar -C "$dist_dir" -czf "$out" "$name"
sha256sum "$out" | tee "$out.sha256"

echo "pack ready: $out"
echo "attach this file to a GitHub Release (e.g. v${version}) so 'omls update' can download it."
