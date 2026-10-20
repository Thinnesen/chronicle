// Command chronicle-exec runs in workflow Pods. "install" copies it into a shared volume and downloads the inputs.
// "run -- command..." runs the main command and reports the outputs. "resource" and "http" run those templates.
package main

import (
	"fmt"
	"os"

	"github.com/Thinnesen/chronicle/internal/executor"
)

func main() {
	switch {
	case len(os.Args) == 3 && os.Args[1] == "install":
		os.Exit(executor.InstallMain(os.Args[2], os.Stderr))
	case len(os.Args) == 2 && os.Args[1] == "resource":
		os.Exit(executor.RunResourceMain(os.Stderr))
	case len(os.Args) == 2 && os.Args[1] == "http":
		os.Exit(executor.RunHTTPMain(os.Stdout, os.Stderr))
	case len(os.Args) >= 3 && os.Args[1] == "run" && os.Args[2] == "--":
		os.Exit(executor.Run(os.Args[3:], os.Stdout, os.Stderr))
	default:
		fmt.Fprintln(os.Stderr, "usage: chronicle-exec install <path> | run -- <command> [args...] | resource | http")
		os.Exit(2)
	}
}
