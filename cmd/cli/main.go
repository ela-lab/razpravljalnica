package main

import (
	"log"
)

func main() {
	if err := RunCLI(); err != nil {
		log.Fatalf("CLI error: %v", err)
	}
}
