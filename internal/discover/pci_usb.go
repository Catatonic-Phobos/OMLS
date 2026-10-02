package discover

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func collectPCI(root string) ([]rdl.Resource, []string) {
	warns := []string{}
	base := pathJoin(root, "/sys/bus/pci/devices")
	entries, err := os.ReadDir(base)
	if err != nil {
		warns = append(warns, "PCI sysfs unavailable (expected on many WSL setups): "+err.Error())
		return nil, warns
	}

	out := []rdl.Resource{}
	gpuIdx := 0
	pciIdx := 0
	for _, e := range entries {
		name := e.Name()
		dev := filepath.Join(base, name)
		vendor, _ := readTrim(filepath.Join(dev, "vendor"))
		device, _ := readTrim(filepath.Join(dev, "device"))
		classCode, _ := readTrim(filepath.Join(dev, "class"))
		subsystemVendor, _ := readTrim(filepath.Join(dev, "subsystem_vendor"))
		subsystemDevice, _ := readTrim(filepath.Join(dev, "subsystem_device"))

		attrs := map[string]any{
			"address": name,
		}
		if vendor != "" {
			attrs["vendor"] = vendor
		}
		if device != "" {
			attrs["device"] = device
		}
		if classCode != "" {
			attrs["pci_class"] = classCode
		}
		if subsystemVendor != "" {
			attrs["subsystem_vendor"] = subsystemVendor
		}
		if subsystemDevice != "" {
			attrs["subsystem_device"] = subsystemDevice
		}

		// Display class 0x03xxxx → GPU/graphics via PCI class only (no vendor SDKs).
		isGPU := isPCIGraphicsClass(classCode)
		if isGPU {
			caps := []string{"graphics"}
			// Many GPUs also expose compute; advertise soft capability without claiming CUDA/ROCm.
			caps = append(caps, "compute")
			id := "gpu" + strconv.Itoa(gpuIdx)
			gpuIdx++
			out = append(out, rdl.Resource{
				ID:           id,
				Kind:         "device",
				Class:        "graphics",
				Capabilities: caps,
				Attrs:        attrs,
			})
			continue
		}

		id := "pci" + strconv.Itoa(pciIdx)
		pciIdx++
		out = append(out, rdl.Resource{
			ID:           id,
			Kind:         "device",
			Class:        "pci",
			Capabilities: []string{"pci"},
			Attrs:        attrs,
		})
	}
	if len(out) == 0 {
		warns = append(warns, "PCI bus empty or inaccessible")
	}
	return out, warns
}

func isPCIGraphicsClass(classCode string) bool {
	c := strings.TrimSpace(strings.ToLower(classCode))
	c = strings.TrimPrefix(c, "0x")
	if len(c) < 2 {
		return false
	}
	// class is often 0x030000 — first byte (after optional 0x) is base class.
	// Accept 03xxxxxx forms.
	if strings.HasPrefix(c, "03") {
		return true
	}
	return false
}

func collectUSB(root string) ([]rdl.Resource, []string) {
	warns := []string{}
	base := pathJoin(root, "/sys/bus/usb/devices")
	entries, err := os.ReadDir(base)
	if err != nil {
		warns = append(warns, "USB sysfs unavailable: "+err.Error())
		return nil, warns
	}

	out := []rdl.Resource{}
	idx := 0
	for _, e := range entries {
		name := e.Name()
		// Prefer device nodes that look like bus-port (e.g. 1-1), skip usbN hubs-as-bus and endpoints.
		if strings.HasPrefix(name, "usb") {
			continue
		}
		if strings.Contains(name, ":") {
			continue // interface nodes
		}
		dev := filepath.Join(base, name)
		idVendor, _ := readTrim(filepath.Join(dev, "idVendor"))
		idProduct, _ := readTrim(filepath.Join(dev, "idProduct"))
		if idVendor == "" && idProduct == "" {
			continue
		}
		attrs := map[string]any{
			"sysfs": name,
		}
		if idVendor != "" {
			attrs["id_vendor"] = idVendor
		}
		if idProduct != "" {
			attrs["id_product"] = idProduct
		}
		if manu, err := readTrim(filepath.Join(dev, "manufacturer")); err == nil && manu != "" {
			attrs["manufacturer"] = manu
		}
		if prod, err := readTrim(filepath.Join(dev, "product")); err == nil && prod != "" {
			attrs["product"] = prod
		}
		if speed, err := readTrim(filepath.Join(dev, "speed")); err == nil && speed != "" {
			attrs["speed_mbps"] = speed
		}

		id := "usb" + strconv.Itoa(idx)
		idx++
		out = append(out, rdl.Resource{
			ID:           id,
			Kind:         "device",
			Class:        "usb",
			Capabilities: []string{"usb"},
			Attrs:        attrs,
		})
	}
	if len(out) == 0 {
		warns = append(warns, "no USB devices discovered (common in containers/WSL)")
	}
	return out, warns
}
