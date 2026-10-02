// Package sandbox is the OMLS 0.8 userspace driver-sandbox stub (VFIO/UIO probe).
// It never requires real VFIO hardware; typical VMs degrade with clear warnings.
package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

const CapabilitySandbox = "sandbox"
const CapabilityVFIO = "vfio"
const CapabilityUIO = "uio"

// Options controls probing.
type Options struct {
	// SysRoot prefixes sysfs/dev paths for fixtures. Empty = host.
	SysRoot string
	// AllowClaim enables simulated claim (still never binds real devices in v0).
	AllowClaim bool
}

// Backend is a detected sandbox mechanism.
type Backend string

const (
	BackendNone Backend = "none"
	BackendVFIO Backend = "vfio"
	BackendUIO  Backend = "uio"
	BackendBoth Backend = "vfio+uio"
)

// Device is a sandboxed (or sandbox-candidate) device resource.
type Device struct {
	ID          string         `json:"id" yaml:"id"`
	Backend     Backend        `json:"backend" yaml:"backend"`
	SysPath     string         `json:"sys_path,omitempty" yaml:"sys_path,omitempty"`
	Name        string         `json:"name,omitempty" yaml:"name,omitempty"`
	Attrs       map[string]any `json:"attrs,omitempty" yaml:"attrs,omitempty"`
	Claimable   bool           `json:"claimable" yaml:"claimable"`
	Unsupported string         `json:"unsupported,omitempty" yaml:"unsupported,omitempty"`
}

// ProbeResult is the outcome of a best-effort sandbox probe.
type ProbeResult struct {
	Backend     Backend   `json:"backend" yaml:"backend"`
	VFIOPresent bool      `json:"vfio_present" yaml:"vfio_present"`
	UIOPresent  bool      `json:"uio_present" yaml:"uio_present"`
	Devices     []Device  `json:"devices" yaml:"devices"`
	Warnings    []string  `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	ProbedAt    time.Time `json:"probed_at" yaml:"probed_at"`
}

// ClaimResult is a dry-run / simulated claim outcome.
type ClaimResult struct {
	DeviceID  string   `json:"device_id" yaml:"device_id"`
	Backend   Backend  `json:"backend" yaml:"backend"`
	OK        bool     `json:"ok" yaml:"ok"`
	Simulated bool     `json:"simulated" yaml:"simulated"`
	Message   string   `json:"message" yaml:"message"`
	Warnings  []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

func joinRoot(root, abs string) string {
	if root == "" {
		return abs
	}
	return filepath.Join(root, strings.TrimPrefix(abs, "/"))
}

// Probe inspects /sys (and /dev) for VFIO/UIO presence and candidate devices.
func Probe(opt Options) ProbeResult {
	root := strings.TrimRight(opt.SysRoot, "/")
	res := ProbeResult{
		Backend:  BackendNone,
		Devices:  []Device{},
		Warnings: []string{},
		ProbedAt: time.Now().UTC(),
	}

	vfioSys := joinRoot(root, "/sys/bus/vfio/devices")
	vfioClass := joinRoot(root, "/sys/class/vfio")
	vfioDev := joinRoot(root, "/dev/vfio")
	uioSys := joinRoot(root, "/sys/class/uio")
	uioDev := joinRoot(root, "/dev")

	res.VFIOPresent = dirExists(vfioSys) || dirExists(vfioClass) || dirExists(vfioDev)
	res.UIOPresent = dirExists(uioSys) || hasUIODevNodes(uioDev)

	switch {
	case res.VFIOPresent && res.UIOPresent:
		res.Backend = BackendBoth
	case res.VFIOPresent:
		res.Backend = BackendVFIO
	case res.UIOPresent:
		res.Backend = BackendUIO
	default:
		res.Backend = BackendNone
		res.Warnings = append(res.Warnings,
			"no VFIO/UIO sysfs or device nodes detected (typical on cloud/lab VMs without passthrough); sandbox remains dry-run only")
	}

	if res.VFIOPresent {
		devs, warns := listVFIO(root, vfioSys, vfioClass)
		res.Devices = append(res.Devices, devs...)
		res.Warnings = append(res.Warnings, warns...)
		if len(devs) == 0 {
			res.Warnings = append(res.Warnings, "VFIO present but no bound devices listed under sysfs")
		}
	}
	if res.UIOPresent {
		devs, warns := listUIO(root, uioSys)
		res.Devices = append(res.Devices, devs...)
		res.Warnings = append(res.Warnings, warns...)
		if len(devs) == 0 {
			res.Warnings = append(res.Warnings, "UIO class present but no uioN devices found")
		}
	}

	sort.SliceStable(res.Devices, func(i, j int) bool { return res.Devices[i].ID < res.Devices[j].ID })
	return res
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func hasUIODevNodes(devDir string) bool {
	entries, err := os.ReadDir(devDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "uio") {
			return true
		}
	}
	return false
}

func listVFIO(root, devicesPath, classPath string) ([]Device, []string) {
	warns := []string{}
	out := []Device{}
	seen := map[string]bool{}

	add := func(name, sysPath string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, Device{
			ID:          "vfio-" + sanitizeID(name),
			Backend:     BackendVFIO,
			SysPath:     sysPath,
			Name:        name,
			Attrs:       map[string]any{"kind": "vfio"},
			Claimable:   false, // v0 never really binds
			Unsupported: "real VFIO bind/unbind is not implemented in OMLS 0.8 (userspace stub)",
		})
	}

	if entries, err := os.ReadDir(devicesPath); err == nil {
		for _, e := range entries {
			add(e.Name(), filepath.Join(devicesPath, e.Name()))
		}
	} else if !os.IsNotExist(err) {
		warns = append(warns, "vfio devices: "+err.Error())
	}
	if entries, err := os.ReadDir(classPath); err == nil {
		for _, e := range entries {
			add(e.Name(), filepath.Join(classPath, e.Name()))
		}
	}
	_ = root
	return out, warns
}

func listUIO(root, classPath string) ([]Device, []string) {
	warns := []string{}
	out := []Device{}
	entries, err := os.ReadDir(classPath)
	if err != nil {
		if !os.IsNotExist(err) {
			warns = append(warns, "uio class: "+err.Error())
		}
		return out, warns
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "uio") {
			continue
		}
		sysPath := filepath.Join(classPath, e.Name())
		name := e.Name()
		if b, err := os.ReadFile(filepath.Join(sysPath, "name")); err == nil {
			name = strings.TrimSpace(string(b))
		}
		out = append(out, Device{
			ID:      "uio-" + sanitizeID(e.Name()),
			Backend: BackendUIO,
			SysPath: sysPath,
			Name:    name,
			Attrs: map[string]any{
				"kind":    "uio",
				"uio_dev": e.Name(),
			},
			Claimable:   false,
			Unsupported: "real UIO mmap/claim is not implemented in OMLS 0.8 (userspace stub)",
		})
	}
	_ = root
	return out, warns
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "dev"
	}
	return b.String()
}

// Resources converts probe devices into RDL resources for graph advertisement.
func (p ProbeResult) Resources() []rdl.Resource {
	out := make([]rdl.Resource, 0, len(p.Devices)+1)
	if p.Backend != BackendNone {
		attrs := map[string]any{
			"backend":      string(p.Backend),
			"vfio_present": p.VFIOPresent,
			"uio_present":  p.UIOPresent,
			"mode":         "stub",
		}
		caps := []string{CapabilitySandbox}
		if p.VFIOPresent {
			caps = append(caps, CapabilityVFIO)
		}
		if p.UIOPresent {
			caps = append(caps, CapabilityUIO)
		}
		out = append(out, rdl.Resource{
			ID:           "sandbox0",
			Kind:         "logical",
			Class:        "sandbox",
			Capabilities: caps,
			Attrs:        attrs,
		})
	}
	for _, d := range p.Devices {
		attrs := map[string]any{}
		for k, v := range d.Attrs {
			attrs[k] = v
		}
		attrs["backend"] = string(d.Backend)
		attrs["sys_path"] = d.SysPath
		attrs["name"] = d.Name
		attrs["claimable"] = d.Claimable
		if d.Unsupported != "" {
			attrs["unsupported"] = d.Unsupported
		}
		caps := []string{CapabilitySandbox}
		switch d.Backend {
		case BackendVFIO:
			caps = append(caps, CapabilityVFIO)
		case BackendUIO:
			caps = append(caps, CapabilityUIO)
		}
		out = append(out, rdl.Resource{
			ID:           d.ID,
			Kind:         "device",
			Class:        "sandbox",
			Capabilities: caps,
			Attrs:        attrs,
		})
	}
	return out
}

// Claim simulates binding a device into the sandbox (dry-run). Never touches host drivers.
func Claim(opt Options, deviceID string) ClaimResult {
	probe := Probe(opt)
	res := ClaimResult{
		DeviceID:  deviceID,
		Simulated: true,
		Warnings:  append([]string{}, probe.Warnings...),
	}
	if deviceID == "" {
		res.Message = "device id required"
		return res
	}
	var found *Device
	for i := range probe.Devices {
		if probe.Devices[i].ID == deviceID {
			found = &probe.Devices[i]
			break
		}
	}
	if found == nil {
		// Allow claiming the logical sandbox0 as a no-op demonstration.
		if deviceID == "sandbox0" && probe.Backend != BackendNone {
			res.Backend = probe.Backend
			res.OK = true
			res.Message = "simulated claim of logical sandbox0 (no host driver change)"
			res.Warnings = append(res.Warnings, "OMLS 0.8 claim is dry-run only")
			return res
		}
		res.Backend = probe.Backend
		res.Message = fmt.Sprintf("device %q not found in sandbox probe", deviceID)
		if probe.Backend == BackendNone {
			res.Warnings = append(res.Warnings,
				"unsupported on this host: no VFIO/UIO — claim remains simulated failure")
		}
		return res
	}
	res.Backend = found.Backend
	res.OK = true
	res.Message = fmt.Sprintf("simulated claim of %s via %s (no host bind/unbind performed)", found.ID, found.Backend)
	res.Warnings = append(res.Warnings, found.Unsupported)
	res.Warnings = append(res.Warnings, "OMLS 0.8 claim is dry-run only — safe on lab VMs")
	return res
}

// MergeIntoDocument appends sandbox resources into an RDL document (dedupe by id).
func MergeIntoDocument(doc *rdl.Document, probe ProbeResult) {
	if doc == nil {
		return
	}
	existing := map[string]bool{}
	for _, r := range doc.Resources {
		existing[r.ID] = true
	}
	for _, r := range probe.Resources() {
		if existing[r.ID] {
			continue
		}
		doc.Resources = append(doc.Resources, r)
		existing[r.ID] = true
	}
	if len(probe.Warnings) > 0 {
		doc.Node.Warnings = append(doc.Node.Warnings, probe.Warnings...)
	}
}
