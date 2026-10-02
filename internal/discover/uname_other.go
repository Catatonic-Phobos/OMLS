// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

//go:build !linux

package discover

import "fmt"

func hostUname() (Uname, error) {
	return Uname{}, fmt.Errorf("OMLS 0.1 discovers Linux nodes only")
}
