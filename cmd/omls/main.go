// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package main

import (
	"os"

	"github.com/Catatonic-Phobos/OMLS/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
