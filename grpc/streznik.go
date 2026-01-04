package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ela-lab/razpravljalnica/razpravljalnica"
	"github.com/ela-lab/razpravljalnica/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MessageBoardServer implements the MessageBoard service
type MessageBoardServer struct {
	razpravljalnica.UnimplementedMessageBoardServer

	userStorage         storage.UserStorage
	topicStorage        storage.TopicStorage
	messageStorage      storage.MessageStorage
	likeStorage         storage.LikeStorage
	subscriptionStorage storage.SubscriptionStorage

	// ID counters
	userIDCounter    int64
	topicIDCounter   int64
	messageIDCounter int64
	sequenceCounter  int64

	// Subscription management
	subscribers        map[string]chan *razpravljalnica.MessageEvent // token -> event channel
	subscribersLock    sync.RWMutex
	subscriptionTokens map[string]*SubscriptionInfo // token -> info
	tokensLock         sync.RWMutex

	// Node info
	nodeID  string
	address string
}

// SubscriptionInfo stores information about a subscription token
type SubscriptionInfo struct {
	UserID   int64
	TopicIDs []int64
}

// NewMessageBoardServer creates a new server instance
func NewMessageBoardServer(nodeID, address string) *MessageBoardServer {
	return &MessageBoardServer{
		userStorage:         *storage.NewUserStorage(),
		topicStorage:        *storage.NewTopicStorage(),
		messageStorage:      *storage.NewMessageStorage(),
		likeStorage:         *storage.NewLikeStorage(),
		subscriptionStorage: *storage.NewSubscriptionStorage(),
		subscribers:         make(map[string]chan *razpravljalnica.MessageEvent),
		subscriptionTokens:  make(map[string]*SubscriptionInfo),
		nodeID:              nodeID,
		address:             address,
	}
}

// CreateUser creates a new user
func (s *MessageBoardServer) CreateUser(ctx context.Context, req *razpravljalnica.CreateUserRequest) (*razpravljalnica.User, error) {
	userID := atomic.AddInt64(&s.userIDCounter, 1)

	user := &razpravljalnica.User{
		Id:   userID,
		Name: req.Name,
	}

	var ret struct{}
	if err := s.userStorage.CreateUser(user, &ret); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create user: %v", err)
	}

	log.Printf("Created user: %d - %s", user.Id, user.Name)
	return user, nil
}

// CreateTopic creates a new topic
func (s *MessageBoardServer) CreateTopic(ctx context.Context, req *razpravljalnica.CreateTopicRequest) (*razpravljalnica.Topic, error) {
	topicID := atomic.AddInt64(&s.topicIDCounter, 1)

	topic := &razpravljalnica.Topic{
		Id:   topicID,
		Name: req.Name,
	}

	var ret struct{}
	if err := s.topicStorage.CreateTopic(topic, &ret); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create topic: %v", err)
	}

	log.Printf("Created topic: %d - %s", topic.Id, topic.Name)
	return topic, nil
}

// PostMessage posts a new message to a topic
func (s *MessageBoardServer) PostMessage(ctx context.Context, req *razpravljalnica.PostMessageRequest) (*razpravljalnica.Message, error) {
	// Verify user exists
	var userName string
	if err := s.userStorage.ReadUser(req.UserId, &userName); err != nil || userName == "" {
		return nil, status.Errorf(codes.NotFound, "user not found: %d", req.UserId)
	}

	// Verify topic exists
	var topics []*razpravljalnica.Topic
	if err := s.topicStorage.ReadTopics(&topics); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read topics: %v", err)
	}

	topicExists := false
	for _, t := range topics {
		if t.Id == req.TopicId {
			topicExists = true
			break
		}
	}

	if !topicExists {
		return nil, status.Errorf(codes.NotFound, "topic not found: %d", req.TopicId)
	}

	// Create message
	messageID := atomic.AddInt64(&s.messageIDCounter, 1)
	now := timestamppb.Now()

	message := &razpravljalnica.Message{
		Id:        messageID,
		TopicId:   req.TopicId,
		UserId:    req.UserId,
		Text:      req.Text,
		CreatedAt: now,
		Likes:     0,
	}

	var ret struct{}
	if err := s.messageStorage.CreateMessage(message, &ret); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create message: %v", err)
	}

	log.Printf("Posted message: %d in topic %d by user %d", message.Id, message.TopicId, message.UserId)

	// Broadcast to subscribers
	s.broadcastEvent(&razpravljalnica.MessageEvent{
		SequenceNumber: atomic.AddInt64(&s.sequenceCounter, 1),
		Op:             razpravljalnica.OpType_OP_POST,
		Message:        message,
		EventAt:        timestamppb.Now(),
	}, req.TopicId)

	return message, nil
}

// UpdateMessage updates an existing message
func (s *MessageBoardServer) UpdateMessage(ctx context.Context, req *razpravljalnica.UpdateMessageRequest) (*razpravljalnica.Message, error) {
	// Get existing messages for the topic
	var messages []*razpravljalnica.Message
	if err := s.messageStorage.ReadMessages(req.TopicId, &messages); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read messages: %v", err)
	}

	// Find the message and verify ownership
	var originalMessage *razpravljalnica.Message
	for _, msg := range messages {
		if msg.Id == req.MessageId {
			if msg.UserId != req.UserId {
				return nil, status.Errorf(codes.PermissionDenied, "only the message author can update it")
			}
			originalMessage = msg
			break
		}
	}

	if originalMessage == nil {
		return nil, status.Errorf(codes.NotFound, "message not found: %d", req.MessageId)
	}

	// Update the message
	updatedMessage := &razpravljalnica.Message{
		Id:        originalMessage.Id,
		TopicId:   originalMessage.TopicId,
		UserId:    originalMessage.UserId,
		Text:      req.Text,
		CreatedAt: originalMessage.CreatedAt,
		Likes:     originalMessage.Likes,
	}

	var ret struct{}
	if err := s.messageStorage.UpdateMessage(updatedMessage, &ret); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update message: %v", err)
	}

	log.Printf("Updated message: %d", updatedMessage.Id)

	// Broadcast to subscribers
	s.broadcastEvent(&razpravljalnica.MessageEvent{
		SequenceNumber: atomic.AddInt64(&s.sequenceCounter, 1),
		Op:             razpravljalnica.OpType_OP_UPDATE,
		Message:        updatedMessage,
		EventAt:        timestamppb.Now(),
	}, req.TopicId)

	return updatedMessage, nil
}

// DeleteMessage deletes an existing message
func (s *MessageBoardServer) DeleteMessage(ctx context.Context, req *razpravljalnica.DeleteMessageRequest) (*emptypb.Empty, error) {
	// Get existing messages for the topic
	var messages []*razpravljalnica.Message
	if err := s.messageStorage.ReadMessages(req.TopicId, &messages); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read messages: %v", err)
	}

	// Find the message and verify ownership
	var messageToDelete *razpravljalnica.Message
	for _, msg := range messages {
		if msg.Id == req.MessageId {
			if msg.UserId != req.UserId {
				return nil, status.Errorf(codes.PermissionDenied, "only the message author can delete it")
			}
			messageToDelete = msg
			break
		}
	}

	if messageToDelete == nil {
		return nil, status.Errorf(codes.NotFound, "message not found: %d", req.MessageId)
	}

	// Delete the message
	var ret struct{}
	if err := s.messageStorage.DeleteMessage(req.MessageId, req.TopicId, &ret); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete message: %v", err)
	}

	log.Printf("Deleted message: %d", req.MessageId)

	// Broadcast to subscribers
	s.broadcastEvent(&razpravljalnica.MessageEvent{
		SequenceNumber: atomic.AddInt64(&s.sequenceCounter, 1),
		Op:             razpravljalnica.OpType_OP_DELETE,
		Message:        messageToDelete,
		EventAt:        timestamppb.Now(),
	}, req.TopicId)

	return &emptypb.Empty{}, nil
}

// LikeMessage adds a like to a message
func (s *MessageBoardServer) LikeMessage(ctx context.Context, req *razpravljalnica.LikeMessageRequest) (*razpravljalnica.Message, error) {
	// Get existing messages for the topic
	var messages []*razpravljalnica.Message
	if err := s.messageStorage.ReadMessages(req.TopicId, &messages); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read messages: %v", err)
	}

	// Find the message
	var message *razpravljalnica.Message
	for _, msg := range messages {
		if msg.Id == req.MessageId {
			message = msg
			break
		}
	}

	if message == nil {
		return nil, status.Errorf(codes.NotFound, "message not found: %d", req.MessageId)
	}

	// Add like
	like := &razpravljalnica.Like{
		TopicId:   req.TopicId,
		MessageId: req.MessageId,
		UserId:    req.UserId,
	}

	var ret struct{}
	if err := s.likeStorage.CreateLike(like, &ret); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create like: %v", err)
	}

	// Get updated like count
	var likeCount int64
	if err := s.likeStorage.ReadLikes(req.MessageId, &likeCount); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read likes: %v", err)
	}

	// Update message with new like count
	updatedMessage := &razpravljalnica.Message{
		Id:        message.Id,
		TopicId:   message.TopicId,
		UserId:    message.UserId,
		Text:      message.Text,
		CreatedAt: message.CreatedAt,
		Likes:     int32(likeCount),
	}

	log.Printf("Liked message: %d (now %d likes)", req.MessageId, likeCount)

	// Broadcast to subscribers
	s.broadcastEvent(&razpravljalnica.MessageEvent{
		SequenceNumber: atomic.AddInt64(&s.sequenceCounter, 1),
		Op:             razpravljalnica.OpType_OP_LIKE,
		Message:        updatedMessage,
		EventAt:        timestamppb.Now(),
	}, req.TopicId)

	return updatedMessage, nil
}

// GetSubscriptionNode returns a node to which a subscription can be opened
func (s *MessageBoardServer) GetSubscriptionNode(ctx context.Context, req *razpravljalnica.SubscriptionNodeRequest) (*razpravljalnica.SubscriptionNodeResponse, error) {
	// Generate subscription token
	token := generateToken()

	// Store subscription info
	s.tokensLock.Lock()
	s.subscriptionTokens[token] = &SubscriptionInfo{
		UserID:   req.UserId,
		TopicIDs: req.TopicId,
	}
	s.tokensLock.Unlock()

	log.Printf("Generated subscription token for user %d: %s", req.UserId, token)

	// Return this node (single server implementation)
	return &razpravljalnica.SubscriptionNodeResponse{
		SubscribeToken: token,
		Node: &razpravljalnica.NodeInfo{
			NodeId:  s.nodeID,
			Address: s.address,
		},
	}, nil
}

// ListTopics returns all topics
func (s *MessageBoardServer) ListTopics(ctx context.Context, req *emptypb.Empty) (*razpravljalnica.ListTopicsResponse, error) {
	var topics []*razpravljalnica.Topic
	if err := s.topicStorage.ReadTopics(&topics); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read topics: %v", err)
	}

	return &razpravljalnica.ListTopicsResponse{
		Topics: topics,
	}, nil
}

// GetMessages returns messages from a topic
func (s *MessageBoardServer) GetMessages(ctx context.Context, req *razpravljalnica.GetMessagesRequest) (*razpravljalnica.GetMessagesResponse, error) {
	var messages []*razpravljalnica.Message
	if err := s.messageStorage.ReadMessages(req.TopicId, &messages); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read messages: %v", err)
	}

	// Update like counts for all messages
	for _, msg := range messages {
		var likeCount int64
		if err := s.likeStorage.ReadLikes(msg.Id, &likeCount); err == nil {
			msg.Likes = int32(likeCount)
		}
	}

	// Filter from message ID
	var filteredMessages []*razpravljalnica.Message
	for _, msg := range messages {
		if msg.Id >= req.FromMessageId {
			filteredMessages = append(filteredMessages, msg)
		}
	}

	// Apply limit
	if req.Limit > 0 && len(filteredMessages) > int(req.Limit) {
		filteredMessages = filteredMessages[:req.Limit]
	}

	return &razpravljalnica.GetMessagesResponse{
		Messages: filteredMessages,
	}, nil
}

// SubscribeTopic subscribes to topics and streams message events
func (s *MessageBoardServer) SubscribeTopic(req *razpravljalnica.SubscribeTopicRequest, stream razpravljalnica.MessageBoard_SubscribeTopicServer) error {
	// Verify subscription token
	s.tokensLock.RLock()
	subInfo, exists := s.subscriptionTokens[req.SubscribeToken]
	s.tokensLock.RUnlock()

	if !exists {
		return status.Errorf(codes.Unauthenticated, "invalid subscription token")
	}

	if subInfo.UserID != req.UserId {
		return status.Errorf(codes.PermissionDenied, "token does not match user ID")
	}

	log.Printf("User %d subscribed to topics: %v", req.UserId, req.TopicId)

	// Create subscription channel
	eventChan := make(chan *razpravljalnica.MessageEvent, 100)
	token := req.SubscribeToken

	s.subscribersLock.Lock()
	s.subscribers[token] = eventChan
	s.subscribersLock.Unlock()

	// Register subscriptions
	var ret struct{}
	for _, topicID := range req.TopicId {
		s.subscriptionStorage.CreateSubscription(req.UserId, topicID, &ret)
	}

	// Send historical messages if requested
	if req.FromMessageId > 0 {
		for _, topicID := range req.TopicId {
			var messages []*razpravljalnica.Message
			if err := s.messageStorage.ReadMessages(topicID, &messages); err == nil {
				for _, msg := range messages {
					if msg.Id >= req.FromMessageId {
						// Update like count
						var likeCount int64
						s.likeStorage.ReadLikes(msg.Id, &likeCount)
						msg.Likes = int32(likeCount)

						event := &razpravljalnica.MessageEvent{
							SequenceNumber: atomic.AddInt64(&s.sequenceCounter, 1),
							Op:             razpravljalnica.OpType_OP_POST,
							Message:        msg,
							EventAt:        msg.CreatedAt,
						}
						if err := stream.Send(event); err != nil {
							return err
						}
					}
				}
			}
		}
	}

	// Stream events
	defer func() {
		s.subscribersLock.Lock()
		delete(s.subscribers, token)
		close(eventChan)
		s.subscribersLock.Unlock()
		log.Printf("User %d unsubscribed", req.UserId)
	}()

	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				return nil
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// broadcastEvent sends an event to all relevant subscribers
func (s *MessageBoardServer) broadcastEvent(event *razpravljalnica.MessageEvent, topicID int64) {
	s.subscribersLock.RLock()
	defer s.subscribersLock.RUnlock()

	for token, eventChan := range s.subscribers {
		// Check if this subscriber is subscribed to this topic
		s.tokensLock.RLock()
		subInfo, exists := s.subscriptionTokens[token]
		s.tokensLock.RUnlock()

		if !exists {
			continue
		}

		subscribed := false
		for _, tID := range subInfo.TopicIDs {
			if tID == topicID {
				subscribed = true
				break
			}
		}

		if subscribed {
			select {
			case eventChan <- event:
			case <-time.After(100 * time.Millisecond):
				// Skip if channel is full
				log.Printf("Skipping event for slow subscriber")
			}
		}
	}
}

// generateToken generates a random subscription token
func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Server starts the gRPC server
func Server(url string) {
	lis, err := net.Listen("tcp", url)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	server := NewMessageBoardServer("server-1", url)

	razpravljalnica.RegisterMessageBoardServer(grpcServer, server)

	log.Printf("Server listening on %s", url)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
