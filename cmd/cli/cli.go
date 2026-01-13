package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ela-lab/razpravljalnica/api"
	"github.com/ela-lab/razpravljalnica/internal/client"
	"github.com/urfave/cli/v3"
)

func RunCLI() error {
	cmd := &cli.Command{
		Name:  "razpravljalnica-cli",
		Usage: "Razpravljalnica CLI client",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server",
				Aliases: []string{"s"},
				Value:   "localhost",
				Usage:   "server address",
			},
			&cli.IntFlag{
				Name:    "port",
				Aliases: []string{"p"},
				Value:   9876,
				Usage:   "port number",
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "register",
				Usage: "create a new user",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "name", Required: true, Usage: "user name"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return executeWithService(c, func(service *client.ClientService, c *cli.Command) error {
						user, err := service.CreateUser(c.String("name"))
						if err != nil {
							return err
						}
						fmt.Printf("User created: %s (ID: %d)\n", user.Name, user.Id)
						return nil
					})
				},
			},
			{
				Name:  "create-topic",
				Usage: "create a new topic",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "title", Required: true, Usage: "topic title"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return executeWithService(c, func(service *client.ClientService, c *cli.Command) error {
						topic, err := service.CreateTopic(c.String("title"))
						if err != nil {
							return err
						}
						fmt.Printf("Topic created: %s (ID: %d)\n", topic.Name, topic.Id)
						return nil
					})
				},
			},
			{
				Name:  "post-message",
				Usage: "post a message to a topic",
				Flags: []cli.Flag{
					&cli.Int64Flag{Name: "userId", Required: true, Usage: "user ID"},
					&cli.Int64Flag{Name: "topicId", Required: true, Usage: "topic ID"},
					&cli.StringFlag{Name: "message", Required: true, Usage: "message text"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return executeWithService(c, func(service *client.ClientService, c *cli.Command) error {
						msg, err := service.PostMessage(
							c.Int64("userId"),
							c.Int64("topicId"),
							c.String("message"),
						)
						if err != nil {
							return err
						}
						fmt.Printf("Message posted: %s (ID: %d)\n", msg.Text, msg.Id)
						return nil
					})
				},
			},
			{
				Name:  "update-message",
				Usage: "update an existing message",
				Flags: []cli.Flag{
					&cli.Int64Flag{Name: "userId", Required: true, Usage: "user ID"},
					&cli.Int64Flag{Name: "topicId", Required: true, Usage: "topic ID"},
					&cli.Int64Flag{Name: "messageId", Required: true, Usage: "message ID"},
					&cli.StringFlag{Name: "newText", Required: true, Usage: "new message text"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return executeWithService(c, func(service *client.ClientService, c *cli.Command) error {
						msg, err := service.UpdateMessage(
							c.Int64("userId"),
							c.Int64("topicId"),
							c.Int64("messageId"),
							c.String("newText"),
						)
						if err != nil {
							return err
						}
						fmt.Printf("Message updated: %s (ID: %d)\n", msg.Text, msg.Id)
						return nil
					})
				},
			},
			{
				Name:  "delete-message",
				Usage: "delete a message",
				Flags: []cli.Flag{
					&cli.Int64Flag{Name: "userId", Required: true, Usage: "user ID"},
					&cli.Int64Flag{Name: "topicId", Required: true, Usage: "topic ID"},
					&cli.Int64Flag{Name: "messageId", Required: true, Usage: "message ID"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return executeWithService(c, func(service *client.ClientService, c *cli.Command) error {
						err := service.DeleteMessage(
							c.Int64("userId"),
							c.Int64("topicId"),
							c.Int64("messageId"),
						)
						if err != nil {
							return err
						}
						fmt.Println("Message deleted successfully")
						return nil
					})
				},
			},
			{
				Name:  "like-message",
				Usage: "like a message",
				Flags: []cli.Flag{
					&cli.Int64Flag{Name: "userId", Required: true, Usage: "user ID"},
					&cli.Int64Flag{Name: "topicId", Required: true, Usage: "topic ID"},
					&cli.Int64Flag{Name: "messageId", Required: true, Usage: "message ID"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return executeWithService(c, func(service *client.ClientService, c *cli.Command) error {
						msg, err := service.LikeMessage(
							c.Int64("userId"),
							c.Int64("topicId"),
							c.Int64("messageId"),
						)
						if err != nil {
							return err
						}
						fmt.Printf("Message liked (ID: %d), total likes: %d\n", msg.Id, msg.Likes)
						return nil
					})
				},
			},
			{
				Name:  "list-topics",
				Usage: "list all topics",
				Action: func(ctx context.Context, c *cli.Command) error {
					return executeWithService(c, func(service *client.ClientService, c *cli.Command) error {
						topics, err := service.ListTopics()
						if err != nil {
							return err
						}
						for _, t := range topics {
							fmt.Printf("[ID: %d] %s\n", t.Id, t.Name)
						}
						return nil
					})
				},
			},
			{
				Name:  "get-messages",
				Usage: "get messages from a topic",
				Flags: []cli.Flag{
					&cli.Int64Flag{Name: "topicId", Required: true, Usage: "topic ID"},
					&cli.Int64Flag{Name: "fromMessageId", Value: 0, Usage: "starting message ID"},
					&cli.IntFlag{Name: "limit", Value: 50, Usage: "max number of messages"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return executeWithService(c, func(service *client.ClientService, c *cli.Command) error {
						messages, err := service.GetMessages(
							c.Int64("topicId"),
							c.Int64("fromMessageId"),
							int32(c.Int("limit")),
						)
						if err != nil {
							return err
						}
						for _, m := range messages {
							fmt.Printf("[ID: %d] User %d: %s (Likes: %d) [%s]\n",
								m.Id, m.UserId, m.Text, m.Likes, m.CreatedAt.AsTime().Format("2006-01-02 15:04:05"))
						}
						return nil
					})
				},
			},
			{
				Name:  "subscribe",
				Usage: "subscribe to multiple topics and receive real-time updates",
				Flags: []cli.Flag{
					&cli.Int64Flag{Name: "userId", Required: true, Usage: "user ID"},
					&cli.Int64SliceFlag{Name: "topicIds", Required: true, Usage: "topic IDs to subscribe to (comma-separated)"},
					&cli.Int64Flag{Name: "fromMessageId", Value: 0, Usage: "starting message ID for history"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return executeWithService(c, func(service *client.ClientService, c *cli.Command) error {
						topicIDs := c.Int64Slice("topicIds")
						userID := c.Int64("userId")
						fromMessageID := c.Int64("fromMessageId")

						if len(topicIDs) == 0 {
							return fmt.Errorf("at least one topic ID must be provided")
						}

						// Get subscription tokens and nodes for all topics
						fmt.Printf("Getting subscriptions for topics %v...\n", topicIDs)

						// Build subscriptions list
						subscriptions := make([]struct {
							TopicID   int64
							Token     string
							FromMsgID int64
							NodeAddr  string
						}, len(topicIDs))

						for i, topicID := range topicIDs {
							token, node, err := service.GetSubscriptionNode(userID, topicID)
							if err != nil {
								return fmt.Errorf("failed to get subscription for topic %d: %w", topicID, err)
							}
							subscriptions[i] = struct {
								TopicID   int64
								Token     string
								FromMsgID int64
								NodeAddr  string
							}{
								TopicID:   topicID,
								Token:     token,
								FromMsgID: fromMessageID,
								NodeAddr:  node.Address,
							}
							fmt.Printf("  Topic %d -> Node %s\n", topicID, node.Address)
						}

						fmt.Println("Listening for events from all topics (Ctrl+C to stop)...")

						// Stream events from all subscriptions
						return service.StreamMultipleSubscriptions(ctx, userID, subscriptions, func(event *api.MessageEvent) error {
							fmt.Printf("[%s] %s: User %d in topic %d: %s (Likes: %d)\n",
								event.EventAt.AsTime().Format("15:04:05"),
								event.Op.String(),
								event.Message.UserId,
								event.Message.TopicId,
								event.Message.Text,
								event.Message.Likes,
							)
							return nil
						})
					})
				},
			},
		},
	}

	return cmd.Run(context.Background(), os.Args)
}

// executeWithService is a helper to connect to the service and execute an action
func executeWithService(c *cli.Command, action func(*client.ClientService, *cli.Command) error) error {
	serverAddr := c.String("server")
	port := c.Int("port")
	url := fmt.Sprintf("%s:%d", serverAddr, port)

	fmt.Printf("gRPC client connecting to %v\n", url)

	service, err := client.NewClientService(url, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer service.Close()

	return action(service, c)
}
