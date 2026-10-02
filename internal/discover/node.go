package discover

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func pathJoin(root, p string) string {
	if root == "" {
		return p
	}
	return filepath.Join(root, strings.TrimPrefix(p, "/"))
}

func readTrim(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func readFirstLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if sc.Scan() {
		return strings.TrimSpace(sc.Text()), nil
	}
	return "", sc.Err()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// stableNodeID prefers /etc/machine-id, then D-Bus machine-id, then a hash fallback.
func stableNodeID(root string) (id string, source string, warn string) {
	candidates := []struct {
		path   string
		source string
	}{
		{pathJoin(root, "/etc/machine-id"), "etc-machine-id"},
		{pathJoin(root, "/var/lib/dbus/machine-id"), "dbus-machine-id"},
	}
	for _, c := range candidates {
		v, err := readTrim(c.path)
		if err == nil && v != "" && v != "uninitialized" {
			return v, c.source, ""
		}
	}

	host, _ := os.Hostname()
	seed := host + "|" + runtime.GOARCH + "|" + runtime.GOOS
	sum := sha1.Sum([]byte(seed))
	return "omls-" + hex.EncodeToString(sum[:8]), "fallback-hash",
		"machine-id unavailable; using fallback hash of hostname+arch"
}

func collectOS(root string) (rdl.OSInfo, []string) {
	warns := []string{}
	info := rdl.OSInfo{
		Family: "linux",
		// Prefer the uname-style machine string (x86_64/aarch64) so node.os.arch
		// matches cpu0.attrs.arch. runtime.GOARCH (amd64/arm64) is Go's label.
		Arch: machineArch(root),
	}
	if k, err := readTrim(pathJoin(root, "/proc/sys/kernel/osrelease")); err == nil {
		info.Kernel = k
	} else if u, err := readTrim(pathJoin(root, "/proc/version")); err == nil {
		fields := strings.Fields(u)
		if len(fields) >= 3 {
			info.Kernel = fields[2]
		}
	} else {
		warns = append(warns, "kernel version unavailable")
	}

	pretty := ""
	osRelease := pathJoin(root, "/etc/os-release")
	if f, err := os.Open(osRelease); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				pretty = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
				break
			}
		}
	}
	if pretty == "" {
		pretty = "linux"
		warns = append(warns, "/etc/os-release PRETTY_NAME missing")
	}
	info.Pretty = pretty
	return info, warns
}

func detectVirt(root string) (string, []string) {
	warns := []string{}

	// WSL markers from the inspected rootfs. Host env vars only count when
	// discovering the live machine (empty SysRoot), not fixtures/chroots.
	if fileExists(pathJoin(root, "/proc/sys/fs/binfmt_misc/WSLInterop")) {
		return "wsl", warns
	}
	if root == "" && (os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSLENV") != "") {
		return "wsl", warns
	}
	if ver, err := readTrim(pathJoin(root, "/proc/version")); err == nil {
		low := strings.ToLower(ver)
		if strings.Contains(low, "microsoft") || strings.Contains(low, "wsl") {
			return "wsl", warns
		}
	}

	// systemd-detect-virt style: DMI product / sysfs hypervisor.
	if fileExists(pathJoin(root, "/proc/xen")) {
		return "xen", warns
	}
	if dmi, err := readTrim(pathJoin(root, "/sys/class/dmi/id/product_name")); err == nil {
		low := strings.ToLower(dmi)
		switch {
		case strings.Contains(low, "virtualbox"):
			return "virtualbox", warns
		case strings.Contains(low, "vmware"):
			return "vmware", warns
		case strings.Contains(low, "kvm"), strings.Contains(low, "bochs"):
			return "kvm", warns
		case strings.Contains(low, "hyper-v"), strings.Contains(low, "virtual machine"):
			return "hyperv", warns
		}
	}
	if cpuinfo, err := os.ReadFile(pathJoin(root, "/proc/cpuinfo")); err == nil {
		low := strings.ToLower(string(cpuinfo))
		if strings.Contains(low, "hypervisor") {
			return "hypervisor", warns
		}
	}
	if fileExists("/.dockerenv") || fileExists(pathJoin(root, "/.dockerenv")) {
		return "docker", warns
	}

	// cgroup / container hints
	if cg, err := readTrim(pathJoin(root, "/proc/1/cgroup")); err == nil {
		if strings.Contains(cg, "docker") || strings.Contains(cg, "containerd") || strings.Contains(cg, "kubepods") {
			return "container", warns
		}
	}

	return "bare", warns
}
