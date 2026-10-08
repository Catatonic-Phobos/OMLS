#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
service_user="$(id -un)"
build_dir="$(mktemp -d)"
trap 'rm -rf "$build_dir"' EXIT

cd "$repo_root"
go build -o "$build_dir/omls" ./cmd/omls

# This laptop has an Intel GPU whose OpenCL userspace runtime is provided by Mesa.
needs_mesa=false
for device in /sys/bus/pci/devices/*; do
  [[ -r "$device/class" && -r "$device/vendor" ]] || continue
  class="$(<"$device/class")"
  vendor="$(<"$device/vendor")"
  if [[ "$class" == 0x03* && ( "$vendor" == 0x8086 || "$vendor" == 0x1002 ) ]]; then
    needs_mesa=true
    break
  fi
done
if $needs_mesa && [[ ! -e /etc/OpenCL/vendors/rusticl.icd && ! -e /usr/share/OpenCL/vendors/rusticl.icd ]]; then
  sudo apt-get install -y --no-install-recommends mesa-opencl-icd
fi

sudo install -D -m 0755 "$build_dir/omls" /usr/local/bin/omls
sudo ln -sfn /usr/local/bin/omls /usr/local/bin/omlsd
sed "s/@OMLS_USER@/$service_user/g" "$repo_root/scripts/systemd/omls.service.in" \
  | sudo install -D -m 0644 /dev/stdin /etc/systemd/system/omls.service
sudo systemctl daemon-reload
# The older master/agent units bind the same fabric port. Stop them so the
# cluster daemon is the process that starts at boot.
sudo systemctl disable --now omls-master.service omls-agent.service 2>/dev/null || true
sudo systemctl enable --now omls.service

printf 'OMLS node enabled. Check with: systemctl status omls\n'
printf 'Logs: journalctl -u omls -f\n'
printf 'Cluster: omls status && omls nodes\n'
