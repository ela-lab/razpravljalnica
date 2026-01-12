package main

import (
	"flag"
	"log"
)

func main() {
	headURL := flag.String("head", "localhost:9001", "head node address (for writes)")
	tailURL := flag.String("tail", "localhost:9003", "tail node address (for reads)")
	flag.Parse()

	if err := RunTUI(*headURL, *tailURL); err != nil {
		log.Fatalf("TUI error: %v", err)
	}
}
