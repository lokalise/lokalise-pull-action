package main

import (
	"fmt"
	"os"
)

func main() {
	// stdout lines
	fmt.Println("hello from stdout 1")
	fmt.Println("hello from stdout 2")
	// stderr lines
	fmt.Fprintln(os.Stderr, "warn from stderr 1")
	fmt.Fprintln(os.Stderr, "warn from stderr 2")
	// final error line that we look for in tail
	fmt.Fprintln(os.Stderr, "polling time exceeded limit")
	os.Exit(1)
}
