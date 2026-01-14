package main

import (
	"flag"
	"fmt"
	"log"
)

func main() {
	headHost := flag.String("s", "localhost", "head server address (fallback if control plane unavailable)")
	headPort := flag.Int("p", 9876, "head port (fallback if control plane unavailable)")
	tailHost := flag.String("tail-host", "", "tail server address (optional; defaults to control plane or head)")
	tailPort := flag.Int("tail-port", 0, "tail port (optional; defaults to control plane or head)")
	cpHost := flag.String("control-plane-host", "localhost", "control plane host")
	cpPort := flag.Int("control-plane-port", 5051, "control plane port")
	flag.Parse()

	headURL := fmt.Sprintf("%s:%d", *headHost, *headPort)
	tailURL := ""
	if *tailHost != "" {
		tailURL = fmt.Sprintf("%s:%d", *tailHost, *tailPort)
	}
	cpURL := fmt.Sprintf("%s:%d", *cpHost, *cpPort)

	if err := RunTUI(headURL, tailURL, cpURL); err != nil {
		log.Fatalf("TUI error: %v", err)
	}
}
