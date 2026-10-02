// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package rdl

// persistentFilesystems is the mount allow-list for RDL 0.1.
// Pseudo filesystems such as proc, sysfs, cgroup, and tmpfs are omitted.
var persistentFilesystems = map[string]struct{}{
	"ext2":    {},
	"ext3":    {},
	"ext4":    {},
	"xfs":     {},
	"btrfs":   {},
	"zfs":     {},
	"f2fs":    {},
	"vfat":    {},
	"exfat":   {},
	"ntfs":    {},
	"ntfs3":   {},
	"overlay": {},
}

// PersistentFilesystem reports whether fstype is recorded in an RDL 0.1 mount list.
func PersistentFilesystem(fstype string) bool {
	_, ok := persistentFilesystems[fstype]
	return ok
}
