// Command twinget sends one request to two backends and structurally
// diffs status, headers, and JSON — with noise filters for timestamps
// and ids. See README.md for the full CLI reference.
package main

import (
	"os"

	"github.com/JaydenCJ/twinget/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
