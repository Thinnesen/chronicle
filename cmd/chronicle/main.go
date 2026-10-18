// Command chronicle is the command line tool of chronicle.
package main

import (
	"os"

	"github.com/Thinnesen/chronicle/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
