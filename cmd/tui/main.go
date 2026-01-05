package main

import (
	"flag"
	"fmt"
	"log"
)

func main() {
	serverURL := flag.String("s", "localhost", "server address")
	port := flag.Int("p", 9876, "port number")
	flag.Parse()

	url := fmt.Sprintf("%s:%d", *serverURL, *port)

	if err := RunTUI(url); err != nil {
		log.Fatalf("TUI error: %v", err)
	}
}
