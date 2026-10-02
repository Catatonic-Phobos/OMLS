# RDL 0.1

Resource Description Language 0.1 is the JSON document produced by `omls agent discover`. It describes one Linux node. It does not describe workloads, models, or placement.

The encoding is UTF-8 JSON. A 0.1 reader ignores unknown fields and rejects a document whose `rdl` value is not `0.1`. The normative example locked by tests is [`internal/discover/testdata/node.json`](../internal/discover/testdata/node.json).

## Document

| Field | Required | Meaning |
| --- | --- | --- |
| `rdl` | yes | Schema version. Must be `0.1`. |
| `kind` | yes | Must be `NodeInventory`. |
| `observedAt` | yes | UTC timestamp, RFC 3339, truncated to seconds. |
| `node` | yes | The inventoried machine. |

`node.hostname` is required. `node.machineId` comes from `/etc/machine-id` or `/var/lib/dbus/machine-id`. `node.bootId` comes from `/proc/sys/kernel/random/boot_id`.

## Operating system

`node.os.kernel` and `node.os.arch` come from `uname` on the running kernel. Distribution fields (`id`, `name`, `version`, `versionId`, `prettyName`) come from `/etc/os-release`, then `/usr/lib/os-release`.

## CPU

`node.cpu.logical` is the number of `processor` blocks in `/proc/cpuinfo`. `sockets` and `cores` come from `physical id` and `core id` when those fields exist; otherwise from `/sys/devices/system/cpu/cpuN/topology` when that tree exists; otherwise the node is treated as one socket with one thread per logical CPU. `flags` are the `flags` or `Features` list of the first logical CPU. `threadsPerCore` is set only when `logical` divides evenly by `cores`.

## Memory

Sizes are bytes. Linux `meminfo` reports KiB (1024 bytes). `totalBytes` is `MemTotal`. `availableBytes` is `MemAvailable`, or `MemFree` when `MemAvailable` is absent. `swapTotalBytes` is `SwapTotal`, or 0 when that field is absent.

## Block devices

`node.blockDevices` lists whole disks from `/sys/block`. `sizeBytes` is the `size` file (512-byte sectors) times 512. `rotational` and `removable` are false when the sysfs attribute is missing. `model` is `device/model` with surrounding space removed.

Omitted names: `loopN`, `ramN`, `fdN`, `nbdN`, `zramN`, `srN`, and devices whose `hidden` attribute is `1`. Partitions are not listed; mounts still name partition sources.

Entries are symlinks on a live kernel. Discovery follows them.

## Mounts

`node.mounts` keeps `/proc/mounts` entries whose type is one of:

`ext2`, `ext3`, `ext4`, `xfs`, `btrfs`, `zfs`, `f2fs`, `vfat`, `exfat`, `ntfs`, `ntfs3`, `overlay`.

Order follows the file. Octal escapes such as `\040` are decoded. Pseudo filesystems (`proc`, `sysfs`, `tmpfs`, `cgroup`, and the rest) are omitted.

## Accelerators

`node.accelerators` lists PCI functions whose class base is `0x03` (display controller) or `0x12` (processing accelerator). `vendorId`, `deviceId`, and `class` are lowercase hex from sysfs (`4`, `4`, and `6` digits). `vendor` is a short name for `10de` (nvidia), `1002` and `1022` (amd), and `8086` (intel). Other vendors leave `vendor` empty. `numaNode` is omitted when the attribute is missing or negative. `bus` is `pci`.

Network and bridge functions are not accelerators. Discovery does not run vendor tools and does not report device memory it cannot read from sysfs.

## NUMA

`node.numa` lists `/sys/devices/system/node/nodeN`. `cpus` is the `cpulist` text. `memoryBytes` is that node's `MemTotal`. The array is ordered by id. An absent NUMA tree yields no entries.

## Validation

A valid document has a positive memory total, available memory in `0..total`, at least one logical CPU, `cores` and `sockets` in `1..logical`, and `cores >= sockets`. When `threadsPerCore` is present it satisfies `threadsPerCore * cores == logical`. Accelerator classes are display or processing-accelerator classes. Mount types are in the list above. Duplicate disk names and duplicate NUMA ids are rejected.

Empty block, mount, accelerator, and NUMA lists are valid and are omitted from the encoding.
