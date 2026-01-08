package main

import (
	"flag"
	"fmt"
)

func main() {
	id := flag.String("id", "", "server id")
	port := flag.Int("p", 9876, "port number")
	nextNodeId := flag.String("nextId", "", "next node server id")
	nextPort := flag.Int("nextPort", 0, "next server port")
	flag.Parse()

	url := fmt.Sprintf(":%d", *port)
	nextUrl := fmt.Sprintf(":%d", *nextPort)
	StartServer(*id, url, *nextNodeId, nextUrl)
}
