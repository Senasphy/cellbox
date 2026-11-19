//go:build linux

package native

import (
	"fmt"
	"os"
)

func init() {
	if len(os.Args) == 0 || os.Args[0] != "cellbox-exec" {
		return
	}
	if err := RunExec(3, 4); err != nil {
		fmt.Fprintln(os.Stderr, "cellbox exec:", err)
		os.Exit(127)
	}
	os.Exit(127)
}
