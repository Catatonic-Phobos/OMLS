// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

//go:build linux

package discover

import (
	"fmt"
	"syscall"
)

func hostUname() (Uname, error) {
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil {
		return Uname{}, fmt.Errorf("uname: %w", err)
	}
	return Uname{
		Sysname: utsString(u.Sysname),
		Release: utsString(u.Release),
		Machine: utsString(u.Machine),
	}, nil
}

func utsString(a [65]int8) string {
	b := make([]byte, 0, len(a))
	for _, c := range a {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}
