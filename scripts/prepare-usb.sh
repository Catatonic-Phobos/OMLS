#!/usr/bin/env bash
# Copy the current OMLS release pack onto a USB stick.
# Run on the machine where the pendrive is mounted (e.g. Mintboy).
#
# Usage:
#   ./scripts/prepare-usb.sh /media/phobos/SanDisk
#   ./scripts/prepare-usb.sh              # auto-detect a removable mount
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dest="${1:-}"

pick_mount() {
  local m
  # udisks / desktop mounts
  for m in /media/*/* /run/media/*/* /mnt/*; do
    [[ -d "$m" ]] || continue
    [[ -w "$m" ]] || continue
    # skip obvious system paths
    case "$m" in
      /media/phobos|/run/media/phobos|/mnt) continue ;;
    esac
    printf '%s\n' "$m"
    return 0
  done
  return 1
}

if [[ -z "$dest" ]]; then
  if ! dest="$(pick_mount)"; then
    echo "prepare-usb: pass the pendrive mount point" >&2
    echo "  example: ./scripts/prepare-usb.sh /media/phobos/SanDisk" >&2
    echo "  mounted candidates:" >&2
    ls -d /media/*/* /run/media/*/* /mnt/* 2>/dev/null || true
    exit 1
  fi
  echo "prepare-usb: auto-selected $dest"
fi

if [[ ! -d "$dest" || ! -w "$dest" ]]; then
  echo "prepare-usb: not a writable directory: $dest" >&2
  exit 1
fi

pack="$repo_root/dist/omls-linux-amd64.tar.gz"
if [[ ! -f "$pack" ]]; then
  echo "prepare-usb: building release pack…"
  "$repo_root/scripts/pack-release.sh"
fi

stage="$dest/OMLS"
mkdir -p "$stage"
# Fresh pack contents at <usb>/OMLS/ (flat: omls, install.sh, …)
rm -rf "$stage"/*
tar -C "$stage" --strip-components=1 -xzf "$pack"

cat >"$stage/LEIA-ME.txt" <<EOF
OMLS — pendrive bootstrap
=========================

Neste PC ou em outro Linux com o pendrive montado:

  cd $stage
  # ou: cd /caminho/do/pendrive/OMLS
  sudo ./install.sh

Isso instala /usr/local/bin/omls e habilita omls.service.

Depois:

  omls version
  omls status
  omls update

Não clone o GitHub para dentro de OMLS/OMLS.
EOF

sync
echo "prepare-usb: ready at $stage"
echo "  ls:"
ls -la "$stage"
echo
echo "No alvo:  cd $stage && sudo ./install.sh"
