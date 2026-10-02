// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package discover

import (
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func readOS(root string, uname Uname) (rdl.OS, error) {
	text, err := readOptional(root, "etc/os-release", "usr/lib/os-release")
	if err != nil {
		return rdl.OS{}, err
	}
	fields := parseOSRelease(text)
	return rdl.OS{
		ID:         fields["ID"],
		Name:       fields["NAME"],
		Version:    fields["VERSION"],
		VersionID:  fields["VERSION_ID"],
		PrettyName: fields["PRETTY_NAME"],
		Kernel:     uname.Release,
		Arch:       uname.Machine,
	}, nil
}

func parseOSRelease(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			q := val[0]
			if (q == '"' || q == '\'') && val[len(val)-1] == q {
				val = val[1 : len(val)-1]
			}
		}
		out[key] = val
	}
	return out
}
