package main

import (
	"flag"
	"fmt"
)

func main() {
	id := flag.String("id", "", "server id")
	port := flag.Int("p", 9876, "port number")
	nextPort := flag.Int("nextPort", 0, "next server port")
	flag.Parse()

	url := fmt.Sprintf(":%d", *port)
	nextUrl := fmt.Sprintf(":%d", *nextPort)
	if *nextPort == 0 {
		nextUrl = ""
	}
	StartServer(*id, url, nextUrl)
}
