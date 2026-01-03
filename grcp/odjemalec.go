package main

import (
	"github.com/ela-lab/razpravljalnica/razpravljalnica"
	"context"
	"fmt"
	"time"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Client(url string) {
	fmt.Printf("gRPC client connecting to %v\n", url)
	conn, err := grpc.NewClient(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	// vzpostavimo izvajalno okolje
	contextCRUD, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// vzpostavimo vmesnik gRPC
	grpcClient := razpravljalnica.NewMessageBoardClient(conn)
	
}