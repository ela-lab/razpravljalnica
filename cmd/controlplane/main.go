package main

import (
	"flag"
	"fmt"
)

func main() {
	port := flag.Int("p", 9999, "control plane port")
	flag.Parse()

	url := fmt.Sprintf(":%d", *port)
	StartControlPlane(url)
}
