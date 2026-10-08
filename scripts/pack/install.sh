#!/usr/bin/env bash
# Compatibility entrypoint:
# - In a git checkout: forwards to scripts/install.sh (builds from source).
# - In a release pack this file is replaced by a copy of scripts/install.sh
#   sitting next to the prebuilt ./omls binary.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -x "$here/../install.sh" ]]; then
  exec "$here/../install.sh" "$@"
fi
echo "omls pack: run this from an extracted release pack, or use scripts/install.sh in the git repo" >&2
exit 1
