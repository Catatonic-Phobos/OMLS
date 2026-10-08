#!/usr/bin/env bash
# Install a prebuilt OMLS pack (binary + systemd unit). No Go required.
set -euo pipefail

pack_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
service_user="${OMLS_SERVICE_USER:-$(id -un)}"
bin_src="$pack_root/omls"
unit_src="$pack_root/omls.service.in"

if [[ ! -x "$bin_src" ]]; then
  echo "omls pack: missing executable $bin_src" >&2
  exit 1
fi
if [[ ! -f "$unit_src" ]]; then
  echo "omls pack: missing $unit_src" >&2
  exit 1
fi

run_root() {
  if [[ "$(id -u)" -eq 0 ]]; then
    "$@"
  else
    sudo "$@"
  fi
}

# Stop the daemon before replacing the binary it is executing.
if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files omls.service >/dev/null 2>&1; then
  run_root systemctl stop omls.service 2>/dev/null || true
fi

run_root install -D -m 0755 "$bin_src" /usr/local/bin/omls
run_root ln -sfn /usr/local/bin/omls /usr/local/bin/omlsd

if command -v systemctl >/dev/null 2>&1; then
  sed "s/@OMLS_USER@/$service_user/g" "$unit_src" \
    | run_root install -D -m 0644 /dev/stdin /etc/systemd/system/omls.service
  run_root systemctl daemon-reload
  run_root systemctl disable --now omls-master.service omls-agent.service 2>/dev/null || true
  run_root systemctl enable --now omls.service
  printf 'OMLS node enabled. Check with: systemctl status omls\n'
  printf 'Logs: journalctl -u omls -f\n'
else
  printf 'OMLS binary installed to /usr/local/bin/omls (no systemd on this host)\n'
fi

printf 'Cluster: omls status && omls nodes\n'
printf 'Version: '; /usr/local/bin/omls version || true
