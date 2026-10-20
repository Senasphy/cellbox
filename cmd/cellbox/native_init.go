package main

import (
	"fmt"
	"os"

	nativeruntime "github.com/senasphy/cellbox/internal/runtime/native"
)

func init() {
	if len(os.Args) == 0 || os.Args[0] != "cellbox-init" {
		return
	}
	if err := nativeruntime.RunInit(3, 4); err != nil {
		fmt.Fprintln(os.Stderr, "cellbox init:", err)
		os.Exit(1)
	}
	os.Exit(0)
}
