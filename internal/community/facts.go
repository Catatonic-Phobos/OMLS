package community

import (
	"fmt"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

// FactsFromRDL extracts matcher inputs from a Machine Profile.
func FactsFromRDL(doc *rdl.Document) NodeFacts {
	if doc == nil {
		return NodeFacts{}
	}
	facts := NodeFacts{
		Arch:     doc.Node.OS.Arch,
		Virt:     doc.Node.Virt,
		OSFamily: doc.Node.OS.Family,
		Hostname: doc.Node.Hostname,
	}
	for _, r := range doc.Resources {
		if r.Class != "compute" {
			continue
		}
		if facts.Arch == "" {
			if v, ok := stringAttr(r.Attrs, "arch"); ok {
				facts.Arch = v
			}
		}
		if v, ok := stringAttr(r.Attrs, "model"); ok && v != "" {
			facts.CPUModel = v
			break
		}
		if v, ok := stringAttr(r.Attrs, "model_name"); ok && v != "" {
			facts.CPUModel = v
			break
		}
	}
	return facts
}

func stringAttr(attrs map[string]any, key string) (string, bool) {
	if attrs == nil {
		return "", false
	}
	v, ok := attrs[key]
	if !ok {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return t, t != ""
	default:
		s := strings.TrimSpace(fmt.Sprint(t))
		return s, s != ""
	}
}
