# Installation and packaging requirements

## Current checkout

There is no `.deb` release yet. On apt-based Linux, run `scripts/install-systemd-services.sh` from the repository to build the binary, install the Mesa OpenCL runtime when an Intel or AMD GPU needs it, install the systemd unit files, and enable/start the local master and agent. This is the current bootstrap path; its dependency setup is limited and it may request administrator authentication.

## Future compiled `.deb`

The released installer must inspect the machine through OMLS discovery and make a normal package install sufficient to activate the supported systems that OMLS detects and needs. This requirement applies to every detected subsystem, not just GPUs. The package workflow must:

1. Install the compiled OMLS binary and all required packages for detected, supported hardware and software systems. This includes applicable kernel drivers, userspace drivers, libraries, runtimes, firmware, and supporting tools.
2. Configure and activate those systems: load or enable drivers, set required system configuration, enable relevant services, and initialize each supported subsystem OMLS relies on.
3. Install and enable the appropriate OMLS systemd unit or units, then start them after installation. The agent must start after boot and reconnect to its configured master, restarting after failures.
4. Preserve machine-specific configuration across package upgrades. In single-machine mode, the local master must listen on loopback; joining a remote fabric must use the configured master address and credentials.
5. Support the package manager and driver packages for each supported distribution and hardware vendor instead of assuming that all machines use apt or the same driver package. If a required change cannot be applied without a reboot or unavailable privilege, complete the safe setup steps and clearly report the remaining activation step.

Package lifecycle scripts must be safe to rerun during upgrades and must not overwrite existing node identity, master address, or TLS credentials. Detection, dependency installation, system configuration, and service activation must be idempotent. The package must expose service status and logs through standard `systemctl` and `journalctl` commands.

The current repo script is a limited bootstrap for a local master-plus-agent machine; it currently handles the Intel/AMD Mesa OpenCL dependency and systemd services, not every detected subsystem. It does not yet build or publish a Debian package.
