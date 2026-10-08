//go:build linux && cgo

package gpu

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// PrepareOpenCL installs and enables Mesa's OpenCL runtime for supported
// Intel and AMD GPUs before discovery or agent startup.
func PrepareOpenCL() error {
	vendors, err := hostGPUVendors()
	if err != nil || len(vendors) == 0 {
		return nil
	}
	drivers := make([]string, 0, 2)
	expected := make(map[uint32]string)
	if vendors[0x8086] {
		drivers = append(drivers, "iris")
		expected[0x8086] = "Intel GPU (Mesa Iris)"
	}
	if vendors[0x1002] {
		drivers = append(drivers, "radeonsi")
		expected[0x1002] = "AMD GPU (Mesa RadeonSI)"
	}
	if len(drivers) == 0 {
		return nil
	}

	if !rusticlICDInstalled() {
		if err := installMesaOpenCL(); err != nil {
			return err
		}
	}
	enableRusticlDrivers(drivers)

	devices, err := Devices()
	if err != nil {
		return fmt.Errorf("OpenCL runtime setup completed, but GPU enumeration failed: %w", err)
	}
	for vendorID, name := range expected {
		found := false
		for _, device := range devices {
			if device.VendorID == vendorID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("OpenCL runtime did not expose the detected %s; check the kernel driver and device permissions", name)
		}
	}
	return nil
}

func hostGPUVendors() (map[uint32]bool, error) {
	entries, err := os.ReadDir("/sys/bus/pci/devices")
	if err != nil {
		return nil, err
	}
	vendors := make(map[uint32]bool)
	for _, entry := range entries {
		base := filepath.Join("/sys/bus/pci/devices", entry.Name())
		class, err := os.ReadFile(filepath.Join(base, "class"))
		if err != nil || !isGraphicsClass(string(class)) {
			continue
		}
		vendor, err := os.ReadFile(filepath.Join(base, "vendor"))
		if err != nil {
			continue
		}
		value := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(vendor))), "0x")
		id, err := strconv.ParseUint(value, 16, 32)
		if err == nil {
			vendors[uint32(id)] = true
		}
	}
	return vendors, nil
}

func isGraphicsClass(value string) bool {
	class := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(value)), "0x")
	return strings.HasPrefix(class, "03")
}

func rusticlICDInstalled() bool {
	paths := []string{"/etc/OpenCL/vendors/rusticl.icd", "/usr/share/OpenCL/vendors/rusticl.icd"}
	for _, dir := range filepath.SplitList(os.Getenv("OCL_ICD_VENDORS")) {
		if dir != "" {
			paths = append(paths, filepath.Join(dir, "rusticl.icd"))
		}
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func enableRusticlDrivers(drivers []string) {
	configured := strings.Split(os.Getenv("RUSTICL_ENABLE"), ",")
	for _, driver := range drivers {
		found := false
		for _, value := range configured {
			if strings.SplitN(strings.TrimSpace(value), ":", 2)[0] == driver {
				found = true
				break
			}
		}
		if !found {
			configured = append(configured, driver)
		}
	}
	_ = os.Setenv("RUSTICL_ENABLE", strings.Join(configured, ","))
}

func installMesaOpenCL() error {
	if !aptBasedOS() {
		return fmt.Errorf("a Mesa OpenCL runtime is needed for the detected Intel/AMD GPU; automatic setup currently supports apt-based Linux")
	}
	fmt.Fprintln(os.Stderr, "omls: installing mesa-opencl-icd for the detected GPU")
	if os.Geteuid() == 0 {
		return runPackageCommand("apt-get", "install", "-y", "mesa-opencl-icd")
	}
	return runPackageCommand("sudo", "apt-get", "install", "-y", "mesa-opencl-icd")
}

func runPackageCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("automatic OpenCL runtime installation failed: %w", err)
	}
	return nil
}

func aptBasedOS() bool {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			id := strings.Trim(strings.TrimPrefix(line, "ID="), "\"'")
			if id == "ubuntu" || id == "debian" || id == "linuxmint" || id == "pop" {
				return true
			}
		}
		if strings.HasPrefix(line, "ID_LIKE=") {
			for _, id := range strings.Fields(strings.Trim(strings.TrimPrefix(line, "ID_LIKE="), "\"'")) {
				if id == "ubuntu" || id == "debian" {
					return true
				}
			}
		}
	}
	return false
}
