package main

import (
	"flag"
	"fmt"
)

func main() {
	id := flag.String("id", "", "server id")
	port := flag.Int("p", 9876, "port number")
	nextNodeId := flag.String("nextId", "", "next node server id")
	flag.Parse()

	url := fmt.Sprintf(":%d", *port)
	StartServer(*id, url, *nextNodeId)
}
