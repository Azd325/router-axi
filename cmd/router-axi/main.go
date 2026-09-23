package main

import (
	"fmt"
	"io"
	"os"
)

var version = "dev"

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		if _, err := fmt.Fprintf(stdout, "version: %s\n", version); err != nil {
			return 1
		}
		return 0
	}

	for _, line := range []string{
		"error:",
		"  code: not_implemented",
		"  message: router inspection is not implemented yet",
		"help[1]: router-axi version",
	} {
		if _, err := fmt.Fprintln(stderr, line); err != nil {
			return 1
		}
	}
	return 2
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
