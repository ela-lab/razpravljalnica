package main

import (
	"context"
	"fmt"
	"time"
	"os"
	
	"github.com/ela-lab/razpravljalnica/razpravljalnica"
	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

func Client(url string) {
	fmt.Printf("gRPC client connecting to %v\n", url)
	conn, err := grpc.NewClient(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	// vzpostavimo izvajalno okolje
	_, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// vzpostavimo vmesnik gRPC
	grpcClient := razpravljalnica.NewMessageBoardClient(conn)
	cmd := &cli.Command {
		Name: "client",
		Commands: []*cli.Command{
			{
				Name:  "register",
				Usage: "creating a new user",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "name", Required: true},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					user, err := grpcClient.CreateUser(ctx, &razpravljalnica.CreateUserRequest{Name: c.String("name")})
					if err != nil {
						return err
					}
					fmt.Printf("User created: %s (ID: %d)\n", user.Name, user.Id)
					return nil
				},
			},
			{
				Name:  "create-topic",
				Usage: "creating a new topic",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "title", Required: true},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					topic, err := grpcClient.CreateTopic(ctx, &razpravljalnica.CreateTopicRequest{Name: c.String("title")})
					if err != nil {
						return err
					}
					fmt.Printf("Topic created: %s (ID: %d)\n", topic.Name, topic.Id)
					return nil
				},
			},
			{
				Name:  "post-message",
				Usage: "post a message to a topic",
				Flags: []cli.Flag{
					&cli.Int64Flag{Name: "userId", Required: true},
					&cli.Int64Flag{Name: "topicId", Required: true},
					&cli.StringFlag{Name: "message", Required: true},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					msg, err := grpcClient.PostMessage(ctx, &razpravljalnica.PostMessageRequest{
						UserId: c.Int64("userId"),
						TopicId: c.Int64("topicId"),
						Text: c.String("message"),
					})
					if err != nil {
						return err
					}
					fmt.Printf("Message posted: %s (ID: %d)\n", msg.Text, msg.Id)
					return nil
				},
			},
			{
				Name:  "update-message",
				Usage: "update a message",
				Flags: []cli.Flag{
					&cli.Int64Flag{Name: "userId", Required: true},
					&cli.Int64Flag{Name: "topicId", Required: true},
					&cli.Int64Flag{Name: "messageId", Required: true},
					&cli.StringFlag{Name: "newText", Required: true},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					msg, err := grpcClient.UpdateMessage(ctx, &razpravljalnica.UpdateMessageRequest{
						UserId: c.Int64("userId"),
						TopicId: c.Int64("topicId"),
						MessageId: c.Int64("messageId"),
						Text: c.String("newText"),
					})
					if err != nil {
						return err
					}
					fmt.Printf("Message updated: %s (ID: %d)\n", msg.Text, msg.Id)
					return nil
				},
			},
			{
				Name:  "delete-message",
				Usage: "delete a message",
				Flags: []cli.Flag{
					&cli.Int64Flag{Name: "userId", Required: true},
					&cli.Int64Flag{Name: "topicId", Required: true},
					&cli.Int64Flag{Name: "messageId", Required: true},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					_, err := grpcClient.DeleteMessage(ctx, &razpravljalnica.DeleteMessageRequest{
						UserId: c.Int64("userId"),
						TopicId: c.Int64("topicId"),
						MessageId: c.Int64("messageId"),
					})
					if err != nil {
						return err
					}
					fmt.Println("Message deleted successfully")
					return nil
				},
			},
			{
				Name:  "like-message",
				Usage: "like a message",
				Flags: []cli.Flag{
					&cli.Int64Flag{Name: "userId", Required: true},
					&cli.Int64Flag{Name: "topicId", Required: true},
					&cli.Int64Flag{Name: "messageId", Required: true},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					msg, err := grpcClient.LikeMessage(ctx, &razpravljalnica.LikeMessageRequest{
						UserId: c.Int64("userId"),
						TopicId: c.Int64("topicId"),
						MessageId: c.Int64("messageId"),
					})
					if err != nil {
						return err
					}
					fmt.Printf("Message liked (ID: %d), total likes: %d\n", msg.Id, msg.Likes)
					return nil
				},
			},
			{
				Name:  "list-topics",
				Usage: "list all topics",
				Action: func(ctx context.Context, c *cli.Command) error {
					resp, err := grpcClient.ListTopics(ctx, &emptypb.Empty{})
					if err != nil {
						return err
					}
					for _, t := range resp.Topics {
						fmt.Printf("[ID: %d] %s\n", t.Id, t.Name)
					}
					return nil
				},
			},
			{
				Name:  "get-messages",
				Usage: "get messages from a topic",
				Flags: []cli.Flag{
					&cli.Int64Flag{Name: "topicId", Required: true},
					&cli.Int64Flag{Name: "fromMessageId", Value: 0},
					&cli.IntFlag{Name: "limit", Value: 50},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					resp, err := grpcClient.GetMessages(ctx, &razpravljalnica.GetMessagesRequest{
						TopicId: c.Int64("topicId"),
						FromMessageId: c.Int64("fromMessageId"),
						Limit: int32(c.Int("limit")),
					})
					if err != nil {
						return err
					}
					for _, m := range resp.Messages {
						fmt.Printf("[ID: %d] User %d: %s (Likes: %d)\n", m.Id, m.UserId, m.Text, m.Likes)
					}
					return nil
				},
			},
		},
	}
	cmd.Run(context.Background(), os.Args)
}