// Command chronicle-exec runs in workflow Pods. "install" copies it into a shared volume.
// "run -- command..." runs the main command and reports the outputs. "resource" runs a resource template.
package main

import (
	"fmt"
	"os"

	"github.com/Thinnesen/chronicle/internal/executor"
)

func main() {
	switch {
	case len(os.Args) == 3 && os.Args[1] == "install":
		if err := executor.Install(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "chronicle-exec:", err)
			os.Exit(1)
		}
	case len(os.Args) == 2 && os.Args[1] == "resource":
		os.Exit(executor.RunResourceMain(os.Stderr))
	case len(os.Args) >= 3 && os.Args[1] == "run" && os.Args[2] == "--":
		os.Exit(executor.Run(os.Args[3:], os.Stdout, os.Stderr))
	default:
		fmt.Fprintln(os.Stderr, "usage: chronicle-exec install <path> | run -- <command> [args...] | resource")
		os.Exit(2)
	}
}
