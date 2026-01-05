package main

import (
	"flag"
	"fmt"
)

func main() {
	port := flag.Int("p", 9876, "port number")
	flag.Parse()

	url := fmt.Sprintf(":%d", *port)
	StartServer(url)
}
