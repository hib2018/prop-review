package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if root, rootErr := repoRoot(); rootErr == nil {
			fmt.Fprintln(os.Stderr, displayMessage(root, err.Error()))
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
