// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

// Package discover inventories one Linux node from stock userspace and returns
// an RDL 0.1 document.
//
// Sources are /proc, /sys, and /etc under Options.Root (default "/"). Discovery
// does not load kernel modules, does not call vendor GPU tools, and does not
// speak to Popcorn or KEEP-Up.
package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

// Uname is the kernel identity used in the OS section.
type Uname struct {
	Sysname string
	Release string
	Machine string
}

// Options controls a discovery pass. Zero values read the live machine.
type Options struct {
	// Root is the filesystem prefix for proc, sys, and etc. Empty means "/".
	Root string
	// Now supplies Document.ObservedAt. Empty means time.Now.
	Now func() time.Time
	// Uname supplies kernel identity. Empty means the running kernel.
	Uname func() (Uname, error)
}

// Discover reads a Linux node and returns a validated RDL 0.1 document.
func Discover(opt Options) (rdl.Document, error) {
	if opt.Root == "" {
		opt.Root = "/"
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Uname == nil {
		opt.Uname = hostUname
	}

	uname, err := opt.Uname()
	if err != nil {
		return rdl.Document{}, err
	}
	if !strings.EqualFold(uname.Sysname, "Linux") {
		return rdl.Document{}, fmt.Errorf("unsupported kernel %q: OMLS 0.1 reads Linux userspace only", uname.Sysname)
	}
	if uname.Release == "" || uname.Machine == "" {
		return rdl.Document{}, fmt.Errorf("uname: kernel release and machine are required")
	}

	hostname, err := readHostname(opt.Root)
	if err != nil {
		return rdl.Document{}, err
	}
	osInfo, err := readOS(opt.Root, uname)
	if err != nil {
		return rdl.Document{}, err
	}
	cpu, err := readCPU(opt.Root)
	if err != nil {
		return rdl.Document{}, err
	}
	cpu.Architecture = uname.Machine
	mem, err := readMemory(opt.Root)
	if err != nil {
		return rdl.Document{}, err
	}
	blocks, err := readBlocks(opt.Root)
	if err != nil {
		return rdl.Document{}, err
	}
	mounts, err := readMounts(opt.Root)
	if err != nil {
		return rdl.Document{}, err
	}
	accels, err := readAccelerators(opt.Root)
	if err != nil {
		return rdl.Document{}, err
	}
	numa, err := readNUMA(opt.Root)
	if err != nil {
		return rdl.Document{}, err
	}
	machineID, err := readOptional(opt.Root, "etc/machine-id", "var/lib/dbus/machine-id")
	if err != nil {
		return rdl.Document{}, err
	}
	bootID, err := readOptional(opt.Root, "proc/sys/kernel/random/boot_id")
	if err != nil {
		return rdl.Document{}, err
	}

	doc := rdl.Document{
		RDL:        rdl.Version,
		Kind:       rdl.KindNodeInventory,
		ObservedAt: opt.Now().UTC().Truncate(time.Second),
		Node: rdl.Node{
			Hostname:     hostname,
			MachineID:    machineID,
			BootID:       bootID,
			OS:           osInfo,
			CPU:          cpu,
			Memory:       mem,
			BlockDevices: blocks,
			Mounts:       mounts,
			Accelerators: accels,
			NUMA:         numa,
		},
	}
	if err := doc.Validate(); err != nil {
		return rdl.Document{}, fmt.Errorf("rdl: %w", err)
	}
	return doc, nil
}

func readHostname(root string) (string, error) {
	s, err := readOptional(root, "proc/sys/kernel/hostname")
	if err != nil {
		return "", err
	}
	if s != "" {
		return s, nil
	}
	if root == "/" {
		host, herr := os.Hostname()
		if herr != nil {
			return "", fmt.Errorf("hostname: %w", herr)
		}
		if strings.TrimSpace(host) != "" {
			return strings.TrimSpace(host), nil
		}
	}
	return "", fmt.Errorf("hostname: proc/sys/kernel/hostname is empty")
}

func join(root, rel string) string {
	if root == "" {
		root = "/"
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

func readRequired(root, rel string) (string, error) {
	b, err := os.ReadFile(join(root, rel))
	if err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	return string(b), nil
}

func readOptional(root string, rels ...string) (string, error) {
	for _, rel := range rels {
		b, err := os.ReadFile(join(root, rel))
		if err == nil {
			return strings.TrimSpace(string(b)), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("%s: %w", rel, err)
		}
	}
	return "", nil
}
