package client

import (
	"context"
	"io"
	"time"

	"github.com/ela-lab/razpravljalnica/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ClientService wraps the gRPC client with business logic
type ClientService struct {
	conn       *grpc.ClientConn
	grpcClient api.MessageBoardClient
	timeout    time.Duration
}

// NewClientService creates a new client service
func NewClientService(url string, timeout time.Duration) (*ClientService, error) {
	conn, err := grpc.NewClient(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	return &ClientService{
		conn:       conn,
		grpcClient: api.NewMessageBoardClient(conn),
		timeout:    timeout,
	}, nil
}

// Close closes the connection
func (cs *ClientService) Close() error {
	return cs.conn.Close()
}

// createContext creates a context with timeout
func (cs *ClientService) createContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), cs.timeout)
}

// CreateUser creates a new user
func (cs *ClientService) CreateUser(name string) (*api.User, error) {
	ctx, cancel := cs.createContext()
	defer cancel()

	return cs.grpcClient.CreateUser(ctx, &api.CreateUserRequest{
		Name: name,
	})
}

// GetUser retrieves a user by ID
func (cs *ClientService) GetUser(userID int64) (*api.User, error) {
	ctx, cancel := cs.createContext()
	defer cancel()

	return cs.grpcClient.GetUser(ctx, &api.GetUserRequest{UserId: userID})
}

// CreateTopic creates a new topic
func (cs *ClientService) CreateTopic(title string) (*api.Topic, error) {
	ctx, cancel := cs.createContext()
	defer cancel()

	return cs.grpcClient.CreateTopic(ctx, &api.CreateTopicRequest{
		Name: title,
	})
}

// PostMessage posts a message to a topic
func (cs *ClientService) PostMessage(userID, topicID int64, text string) (*api.Message, error) {
	ctx, cancel := cs.createContext()
	defer cancel()

	return cs.grpcClient.PostMessage(ctx, &api.PostMessageRequest{
		UserId:  userID,
		TopicId: topicID,
		Text:    text,
	})
}

// UpdateMessage updates an existing message
func (cs *ClientService) UpdateMessage(userID, topicID, messageID int64, newText string) (*api.Message, error) {
	ctx, cancel := cs.createContext()
	defer cancel()

	return cs.grpcClient.UpdateMessage(ctx, &api.UpdateMessageRequest{
		UserId:    userID,
		TopicId:   topicID,
		MessageId: messageID,
		Text:      newText,
	})
}

// DeleteMessage deletes a message
func (cs *ClientService) DeleteMessage(userID, topicID, messageID int64) error {
	ctx, cancel := cs.createContext()
	defer cancel()

	_, err := cs.grpcClient.DeleteMessage(ctx, &api.DeleteMessageRequest{
		UserId:    userID,
		TopicId:   topicID,
		MessageId: messageID,
	})
	return err
}

// LikeMessage adds a like to a message
func (cs *ClientService) LikeMessage(userID, topicID, messageID int64) (*api.Message, error) {
	ctx, cancel := cs.createContext()
	defer cancel()

	return cs.grpcClient.LikeMessage(ctx, &api.LikeMessageRequest{
		UserId:    userID,
		TopicId:   topicID,
		MessageId: messageID,
	})
}

// ListTopics returns all topics
func (cs *ClientService) ListTopics() ([]*api.Topic, error) {
	ctx, cancel := cs.createContext()
	defer cancel()

	resp, err := cs.grpcClient.ListTopics(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return resp.Topics, nil
}

// ListSubscriptions returns topic IDs the user is subscribed to
func (cs *ClientService) ListSubscriptions(userID int64) ([]int64, error) {
	ctx, cancel := cs.createContext()
	defer cancel()

	resp, err := cs.grpcClient.ListSubscriptions(ctx, &api.ListSubscriptionsRequest{UserId: userID})
	if err != nil {
		return nil, err
	}
	return resp.GetTopicId(), nil
}

// GetMessages retrieves messages from a topic
func (cs *ClientService) GetMessages(topicID, fromMessageID int64, limit int32) ([]*api.Message, error) {
	ctx, cancel := cs.createContext()
	defer cancel()

	resp, err := cs.grpcClient.GetMessages(ctx, &api.GetMessagesRequest{
		TopicId:       topicID,
		FromMessageId: fromMessageID,
		Limit:         limit,
	})
	if err != nil {
		return nil, err
	}
	return resp.Messages, nil
}

// GetSubscriptionNode gets a subscription token and node address
func (cs *ClientService) GetSubscriptionNode(userID int64, topicIDs []int64) (string, *api.NodeInfo, error) {
	ctx, cancel := cs.createContext()
	defer cancel()

	resp, err := cs.grpcClient.GetSubscriptionNode(ctx, &api.SubscriptionNodeRequest{
		UserId:  userID,
		TopicId: topicIDs,
	})
	if err != nil {
		return "", nil, err
	}
	return resp.SubscribeToken, resp.Node, nil
}

// SubscribeToTopics subscribes to topic updates (returns a stream client)
func (cs *ClientService) SubscribeToTopics(ctx context.Context, userID int64, topicIDs []int64, token string, fromMessageID int64) (api.MessageBoard_SubscribeTopicClient, error) {
	return cs.grpcClient.SubscribeTopic(ctx, &api.SubscribeTopicRequest{
		UserId:         userID,
		TopicId:        topicIDs,
		SubscribeToken: token,
		FromMessageId:  fromMessageID,
	})
}

// StreamSubscription is a helper to stream events with a callback
func (cs *ClientService) StreamSubscription(ctx context.Context, userID int64, topicIDs []int64, token string, fromMessageID int64, callback func(*api.MessageEvent) error) error {
	stream, err := cs.SubscribeToTopics(ctx, userID, topicIDs, token, fromMessageID)
	if err != nil {
		return err
	}

	for {
		event, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if err := callback(event); err != nil {
			return err
		}
	}
}
