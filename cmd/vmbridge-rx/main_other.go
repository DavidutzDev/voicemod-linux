//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "vmbridge-rx (virtual microphone receiver) is Linux-only")
	os.Exit(1)
}
