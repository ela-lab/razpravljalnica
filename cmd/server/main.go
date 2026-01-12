package main

import (
	"flag"
	"fmt"
	"strings"
)

func main() {
	id := flag.String("id", "", "server id")
	port := flag.Int("p", 9876, "port number")
	nextPort := flag.Int("nextPort", 0, "next server port")
	chainNodes := flag.String("chainNodes", "", "comma-separated list of all node addresses (e.g., localhost:9001,localhost:9002,localhost:9003)")
	flag.Parse()

	url := fmt.Sprintf(":%d", *port)
	nextUrl := fmt.Sprintf(":%d", *nextPort)
	if *nextPort == 0 {
		nextUrl = ""
	}
	
	// Parse chain nodes
	var nodeAddresses []string
	if *chainNodes != "" {
		nodeAddresses = strings.Split(*chainNodes, ",")
	}
	
	StartServer(*id, url, nextUrl, nodeAddresses)
}
