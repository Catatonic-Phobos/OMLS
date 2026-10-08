#!/usr/bin/env bash
# Back-compat name — use scripts/install.sh.
set -euo pipefail
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install.sh" "$@"
