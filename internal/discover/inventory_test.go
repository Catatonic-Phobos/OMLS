// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package discover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMeminfo(t *testing.T) {
	mem, err := parseMeminfo("MemTotal: 2048 kB\nMemFree: 100 kB\nSwapTotal: 0 kB\n")
	if err != nil {
		t.Fatal(err)
	}
	if mem.TotalBytes != 2048*1024 || mem.AvailableBytes != 100*1024 || mem.SwapTotalBytes != 0 {
		t.Fatalf("%+v", mem)
	}
	if _, err := parseMeminfo("MemTotal: 10 MB\n"); err == nil {
		t.Fatal("expected unit error")
	}
	if _, err := parseMeminfo("MemFree: 1 kB\n"); err == nil {
		t.Fatal("expected missing MemTotal")
	}
}

func TestParseOSRelease(t *testing.T) {
	fields := parseOSRelease("# comment\n\nNAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID='24.04'\n")
	if fields["NAME"] != "Ubuntu" || fields["ID"] != "ubuntu" || fields["VERSION_ID"] != "24.04" {
		t.Fatalf("%v", fields)
	}
}

func TestReadOSFallback(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"usr/lib/os-release": "ID=debian\nNAME=Debian\n",
	})
	got, err := readOS(root, Uname{Sysname: "Linux", Release: "6.8.0-test", Machine: "aarch64"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "debian" || got.Kernel != "6.8.0-test" || got.Arch != "aarch64" {
		t.Fatalf("%+v", got)
	}
}

func TestParseMounts(t *testing.T) {
	text := "" +
		"/dev/vda1 / ext4 rw,relatime 0 0\n" +
		"/dev/vda2 /mnt/model\\040cache ext4 rw 0 0\n" +
		"tmpfs /tmp tmpfs rw,nosuid 0 0\n" +
		"overlay / overlay rw 0 0\n"
	got, err := parseMounts(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if got[1].Target != "/mnt/model cache" || got[1].Source != "/dev/vda2" {
		t.Fatalf("%+v", got[1])
	}
	if _, err := parseMounts("only-three fields here\n"); err == nil {
		t.Fatal("expected short line error")
	}
}

func TestReadBlocksSymlinkAndFilters(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "sys/devices/virtio/vda")
	if err := os.MkdirAll(filepath.Join(dev, "queue"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dev, "device"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dev, "size"), "2048\n")
	write(filepath.Join(dev, "removable"), "0\n")
	write(filepath.Join(dev, "queue/rotational"), "1\n")
	write(filepath.Join(dev, "device/model"), "Virtual Disk   \n")

	block := filepath.Join(root, "sys/block")
	if err := os.MkdirAll(block, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../devices/virtio/vda", filepath.Join(block, "vda")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(block, "loop0"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(block, "loop0/size"), "100\n")
	if err := os.MkdirAll(filepath.Join(block, "dm-0"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(block, "dm-0/size"), "10\n")
	write(filepath.Join(block, "dm-0/hidden"), "1\n")

	got, err := readBlocks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	if got[0].Name != "vda" || got[0].SizeBytes != 2048*512 || !got[0].Rotational || got[0].Removable || got[0].Model != "Virtual Disk" {
		t.Fatalf("%+v", got[0])
	}
}

func TestReadAccelerators(t *testing.T) {
	root := t.TempDir()
	pci := filepath.Join(root, "sys/bus/pci/devices")
	net := filepath.Join(pci, "0000:00:04.0")
	if err := os.MkdirAll(net, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(net, "class"), []byte("0x020000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "sys/devices/pci/0000:01:00.0")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"class":     "0x120000\n",
		"vendor":    "0x8086\n",
		"device":    "0x4940\n",
		"numa_node": "-1\n",
	} {
		if err := os.WriteFile(filepath.Join(target, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../../../devices/pci/0000:01:00.0", filepath.Join(pci, "0000:01:00.0")); err != nil {
		t.Fatal(err)
	}
	got, err := readAccelerators(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	acc := got[0]
	if acc.Vendor != "intel" || acc.VendorID != "8086" || acc.DeviceID != "4940" || acc.Class != "120000" || acc.NUMANode != nil {
		t.Fatalf("%+v", acc)
	}
	if err := os.WriteFile(filepath.Join(target, "class"), []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAccelerators(root); err == nil || !strings.Contains(err.Error(), "class") {
		t.Fatalf("got %v", err)
	}
}
