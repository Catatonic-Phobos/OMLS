// Package discover collects best-effort Linux hardware facts into an RDL Machine Profile.
package discover

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

// Options controls discovery.
type Options struct {
	// SysRoot prefixes sysfs/proc paths (useful for fixtures). Empty = host.
	SysRoot string
}

// Result is a validated profile plus human warnings.
type Result struct {
	Document rdl.Document
	Warnings []string
}

// Discover builds an RDL v0 Machine Profile from local Linux sysfs/proc.
// Missing subsystems produce warnings; this function does not panic.
func Discover(opt Options) (Result, error) {
	root := strings.TrimRight(opt.SysRoot, "/")
	warns := []string{}

	doc := rdl.Empty()
	nodeID, idSource, idWarn := stableNodeID(root)
	if idWarn != "" {
		warns = append(warns, idWarn)
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
		warns = append(warns, "hostname unavailable; using unknown")
	}

	osInfo, osWarns := collectOS(root)
	warns = append(warns, osWarns...)

	virt, virtWarns := detectVirt(root)
	warns = append(warns, virtWarns...)

	doc.Node = rdl.Node{
		ID:       nodeID,
		Hostname: hostname,
		OS:       osInfo,
		Virt:     virt,
	}

	resources := []rdl.Resource{}
	transports := []rdl.Transport{}

	cpuRes, cpuWarns := collectCPU(root)
	warns = append(warns, cpuWarns...)
	resources = append(resources, cpuRes...)

	memRes, memWarns := collectMemory(root)
	warns = append(warns, memWarns...)
	resources = append(resources, memRes...)

	storRes, storWarns := collectStorage(root)
	warns = append(warns, storWarns...)
	resources = append(resources, storRes...)

	netRes, netTrans, netWarns := collectNetwork(root)
	warns = append(warns, netWarns...)
	resources = append(resources, netRes...)
	transports = append(transports, netTrans...)

	pciRes, pciWarns := collectPCI(root)
	warns = append(warns, pciWarns...)
	resources = append(resources, pciRes...)

	usbRes, usbWarns := collectUSB(root)
	warns = append(warns, usbWarns...)
	resources = append(resources, usbRes...)

	thermRes, thermWarns := collectThermal(root)
	warns = append(warns, thermWarns...)
	resources = append(resources, thermRes...)

	powRes, powWarns := collectPower(root)
	warns = append(warns, powWarns...)
	resources = append(resources, powRes...)

	doc.Resources = resources
	doc.Transports = transports
	doc.Node.Warnings = append([]string{}, warns...)

	// Annotate node attrs via id source for operators (not schema-required).
	_ = idSource
	_ = runtime.GOARCH

	if err := doc.Validate(); err != nil {
		return Result{}, fmt.Errorf("discovered profile invalid: %w", err)
	}
	return Result{Document: doc, Warnings: warns}, nil
}
