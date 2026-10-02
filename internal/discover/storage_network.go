package discover

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func collectStorage(root string) ([]rdl.Resource, []string) {
	warns := []string{}
	base := pathJoin(root, "/sys/block")
	entries, err := os.ReadDir(base)
	if err != nil {
		warns = append(warns, "/sys/block unreadable: "+err.Error())
		return nil, warns
	}

	out := []rdl.Resource{}
	idx := 0
	for _, e := range entries {
		name := e.Name()
		// Skip virtual/loop/ram by default noise; still allow dm-/nvme/sd/vd/xvd/mmc.
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
			strings.HasPrefix(name, "fd") {
			continue
		}
		devPath := filepath.Join(base, name)
		attrs := map[string]any{"name": name}

		if sizeStr, err := readTrim(filepath.Join(devPath, "size")); err == nil {
			if sectors, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
				attrs["size_bytes"] = sectors * 512
			}
		}
		if ro, err := readTrim(filepath.Join(devPath, "ro")); err == nil {
			attrs["read_only"] = ro == "1"
		}
		if rot, err := readTrim(filepath.Join(devPath, "queue", "rotational")); err == nil {
			attrs["rotational"] = rot == "1"
		}
		if model, err := readTrim(filepath.Join(devPath, "device", "model")); err == nil && model != "" {
			attrs["model"] = model
		}
		if vendor, err := readTrim(filepath.Join(devPath, "device", "vendor")); err == nil && vendor != "" {
			attrs["vendor"] = vendor
		}

		id := "disk" + strconv.Itoa(idx)
		idx++
		out = append(out, rdl.Resource{
			ID:           id,
			Kind:         "device",
			Class:        "storage",
			Capabilities: []string{"storage"},
			Attrs:        attrs,
		})
	}
	if len(out) == 0 {
		warns = append(warns, "no block devices discovered")
	}
	return out, warns
}

func collectNetwork(root string) ([]rdl.Resource, []rdl.Transport, []string) {
	warns := []string{}
	base := pathJoin(root, "/sys/class/net")
	entries, err := os.ReadDir(base)
	if err != nil {
		warns = append(warns, "/sys/class/net unreadable: "+err.Error())
		return nil, nil, warns
	}

	// Optional live addrs from stdlib (host only; skip under fixtures).
	addrByIface := map[string][]string{}
	if root == "" {
		if ifaces, err := net.Interfaces(); err == nil {
			for _, iface := range ifaces {
				addrs, _ := iface.Addrs()
				list := []string{}
				for _, a := range addrs {
					list = append(list, a.String())
				}
				addrByIface[iface.Name] = list
			}
		}
	}

	resources := []rdl.Resource{}
	transports := []rdl.Transport{}
	for _, e := range entries {
		name := e.Name()
		dev := filepath.Join(base, name)
		attrs := map[string]any{"name": name}

		if mtu, err := readTrim(filepath.Join(dev, "mtu")); err == nil {
			if n, err := strconv.Atoi(mtu); err == nil {
				attrs["mtu"] = n
			}
		}
		if mac, err := readTrim(filepath.Join(dev, "address")); err == nil {
			attrs["mac"] = mac
		}
		if oper, err := readTrim(filepath.Join(dev, "operstate")); err == nil {
			attrs["operstate"] = oper
		}
		if speed, err := readTrim(filepath.Join(dev, "speed")); err == nil {
			if n, err := strconv.Atoi(speed); err == nil && n > 0 {
				attrs["speed_mbps"] = n
			}
		}
		if addrs, ok := addrByIface[name]; ok && len(addrs) > 0 {
			attrs["addrs"] = addrs
		}

		typ := "other"
		switch {
		case name == "lo":
			typ = "loopback"
		case strings.HasPrefix(name, "wl"), strings.HasPrefix(name, "wlan"):
			typ = "wifi"
		case strings.HasPrefix(name, "eth"), strings.HasPrefix(name, "en"),
			strings.HasPrefix(name, "em"), strings.HasPrefix(name, "bond"),
			strings.HasPrefix(name, "br"), strings.HasPrefix(name, "veth"):
			typ = "ethernet"
		}

		resources = append(resources, rdl.Resource{
			ID:           "net-" + name,
			Kind:         "device",
			Class:        "network",
			Capabilities: []string{"network"},
			Attrs:        attrs,
		})
		transports = append(transports, rdl.Transport{
			ID:    name,
			Type:  typ,
			Attrs: attrs,
		})
	}
	if len(resources) == 0 {
		warns = append(warns, "no network interfaces discovered")
	}
	return resources, transports, warns
}
