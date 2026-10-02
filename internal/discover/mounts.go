// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package discover

import (
	"fmt"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func readMounts(root string) ([]rdl.Mount, error) {
	text, err := readOptional(root, "proc/mounts")
	if err != nil {
		return nil, err
	}
	if text == "" {
		return nil, nil
	}
	return parseMounts(text)
}

func parseMounts(text string) ([]rdl.Mount, error) {
	var out []rdl.Mount
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return nil, fmt.Errorf("mounts: short line %q", line)
		}
		fstype := unescapeMount(fields[2])
		if !rdl.PersistentFilesystem(fstype) {
			continue
		}
		out = append(out, rdl.Mount{
			Source:  unescapeMount(fields[0]),
			Target:  unescapeMount(fields[1]),
			FSType:  fstype,
			Options: unescapeMount(fields[3]),
		})
	}
	return out, nil
}

func unescapeMount(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			v := (s[i+1]-'0')*64 + (s[i+2]-'0')*8 + (s[i+3] - '0')
			b.WriteByte(v)
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(c byte) bool {
	return c >= '0' && c <= '7'
}
