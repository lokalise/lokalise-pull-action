package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "sleep" {
		// Simulate a long-running process
		fmt.Println("Sleeping for 2 seconds...")
		time.Sleep(2 * time.Second)
	}
}
