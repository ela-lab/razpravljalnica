package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ela-lab/razpravljalnica/api"
	"github.com/ela-lab/razpravljalnica/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/grpc/credentials/insecure"
)

// MessageBoardServer implements the MessageBoard service
type MessageBoardServer struct {
	api.UnimplementedMessageBoardServer
	api.UnimplementedReplicationServiceServer

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
	subscribers        map[string]chan *api.MessageEvent // token -> event channel
	subscribersLock    sync.RWMutex
	subscriptionTokens map[string]*SubscriptionInfo // token -> info
	tokensLock         sync.RWMutex

	// Node info
	nodeID       string
	address      string
	nextAddress  string
	nextReplica  api.ReplicationServiceClient
	eventCounter int64

	// Control plane connection
	controlPlaneClient api.ControlPlaneClient
	controlPlaneConn   *grpc.ClientConn

	// Subscription responsibility (modulo-based)
	myModuloIndex       int32
	totalNodes          int32
	responsibilityLock  sync.RWMutex
	lastResponsibilityUpdate time.Time
	controlPlaneAvailable    bool
}

// SubscriptionInfo stores information about a subscription token
type SubscriptionInfo struct {
	UserID   int64
	TopicIDs []int64
}

// NewMessageBoardServer creates a new server instance
func NewMessageBoardServer(nodeID, address string, nextAddress string) *MessageBoardServer {
	return &MessageBoardServer{
		userStorage:         *storage.NewUserStorage(),
		topicStorage:        *storage.NewTopicStorage(),
		messageStorage:      *storage.NewMessageStorage(),
		likeStorage:         *storage.NewLikeStorage(),
		subscriptionStorage: *storage.NewSubscriptionStorage(),
		subscribers:         make(map[string]chan *api.MessageEvent),
		subscriptionTokens:  make(map[string]*SubscriptionInfo),
		nodeID:              nodeID,
		address:             address,
		nextAddress:         nextAddress,
		eventCounter:        0,
	}
}

// CreateUser creates a new user
func (s *MessageBoardServer) CreateUser(ctx context.Context, req *api.CreateUserRequest) (*api.User, error) {
	if s.nodeID != "head" {
		return nil, status.Errorf(codes.PermissionDenied, "writes only allowed on head")
	}
	userID := atomic.AddInt64(&s.userIDCounter, 1)

	user := &api.User{
		Id:   userID,
		Name: req.Name,
	}

	var ret struct{}
	if err := s.userStorage.CreateUser(user, &ret); err != nil {
		if _, ok := err.(*storage.UserAlreadyExistsError); ok {
			return nil, status.Errorf(codes.AlreadyExists, "user with name '%s' already exists", req.Name)
		}
		return nil, status.Errorf(codes.Internal, "failed to create user: %v", err)
	}

	seq := atomic.AddInt64(&s.eventCounter, 1)

	if s.nextReplica != nil {
		repReq := &api.ReplicationRequest{
			User:           user,
			SequenceNumber: seq,
		}

		resp, err := s.nextReplica.ReplicateOperation(ctx, repReq)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "replication failed: %v", err)
		}
		now := time.Now().Format("15:04:05.000")
    	log.Printf("[%s] [Node %s] Received ack from next node: %d", now, s.nodeID, resp.AckSequenceNumber)

		if resp.AckSequenceNumber != seq {
			return nil, status.Errorf(codes.Internal, "replication out of order")
		}
	}

	log.Printf("Created user: %d - %s", user.Id, user.Name)
	return user, nil
}

// GetUser returns user info by id
func (s *MessageBoardServer) GetUser(ctx context.Context, req *api.GetUserRequest) (*api.User, error) {
	if s.nodeID != "tail" {
		return nil, status.Errorf(codes.PermissionDenied, "reads only allowed on tail")
	}
	var name string
	if err := s.userStorage.ReadUser(req.UserId, &name); err != nil || name == "" {
		return nil, status.Errorf(codes.NotFound, "user not found: %d", req.UserId)
	}

	return &api.User{Id: req.UserId, Name: name}, nil
}

// CreateTopic creates a new topic
func (s *MessageBoardServer) CreateTopic(ctx context.Context, req *api.CreateTopicRequest) (*api.Topic, error) {
	if s.nodeID != "head" {
		return nil, status.Errorf(codes.PermissionDenied, "writes only allowed on head")
	}
	topicID := atomic.AddInt64(&s.topicIDCounter, 1)

	topic := &api.Topic{
		Id:   topicID,
		Name: req.Name,
	}

	var ret struct{}
	if err := s.topicStorage.CreateTopic(topic, &ret); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create topic: %v", err)
	}

	seq := atomic.AddInt64(&s.eventCounter, 1)

	if s.nextReplica != nil {
		repReq := &api.ReplicationRequest{
			Topic:          topic,
			SequenceNumber: seq,
		}

		resp, err := s.nextReplica.ReplicateOperation(ctx, repReq)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "replication failed: %v", err)
		}

		if resp.AckSequenceNumber != seq {
			return nil, status.Errorf(codes.Internal, "replication out of order")
		}
	}

	log.Printf("Created topic: %d - %s", topic.Id, topic.Name)
	return topic, nil
}

// PostMessage posts a new message to a topic
func (s *MessageBoardServer) PostMessage(ctx context.Context, req *api.PostMessageRequest) (*api.Message, error) {
	if s.nodeID != "head" {
		return nil, status.Errorf(codes.PermissionDenied, "writes only allowed on head")
	}
	// Verify user exists
	var userName string
	if err := s.userStorage.ReadUser(req.UserId, &userName); err != nil || userName == "" {
		return nil, status.Errorf(codes.NotFound, "user not found: %d", req.UserId)
	}

	// Verify topic exists
	var topics []*api.Topic
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

	message := &api.Message{
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

	seq := atomic.AddInt64(&s.eventCounter, 1)

	if s.nextReplica != nil {
		repReq := &api.ReplicationRequest{
			Op:             api.OpType_OP_POST,
			Message:        message,
			SequenceNumber: seq,
		}

		resp, err := s.nextReplica.ReplicateOperation(ctx, repReq)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "replication failed: %v", err)
		}

		if resp.AckSequenceNumber != seq {
			return nil, status.Errorf(codes.Internal, "replication out of order")
		}
	}

	log.Printf("Posted message: %d in topic %d by user %d", message.Id, message.TopicId, message.UserId)

	// Broadcast to subscribers
	s.broadcastEvent(&api.MessageEvent{
		SequenceNumber: atomic.AddInt64(&s.sequenceCounter, 1),
		Op:             api.OpType_OP_POST,
		Message:        message,
		EventAt:        timestamppb.Now(),
	}, req.TopicId)

	return message, nil
}

// UpdateMessage updates an existing message
func (s *MessageBoardServer) UpdateMessage(ctx context.Context, req *api.UpdateMessageRequest) (*api.Message, error) {
	if s.nodeID != "head" {
		return nil, status.Errorf(codes.PermissionDenied, "writes only allowed on head")
	}
	// Get existing messages for the topic
	var messages []*api.Message
	if err := s.messageStorage.ReadMessages(req.TopicId, &messages); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read messages: %v", err)
	}

	// Find the message and verify ownership
	var originalMessage *api.Message
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
	updatedMessage := &api.Message{
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

	seq := atomic.AddInt64(&s.eventCounter, 1)

	if s.nextReplica != nil {
		repReq := &api.ReplicationRequest{
			Op:             api.OpType_OP_UPDATE,
			Message:        updatedMessage,
			SequenceNumber: seq,
		}

		resp, err := s.nextReplica.ReplicateOperation(ctx, repReq)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "replication failed: %v", err)
		}

		if resp.AckSequenceNumber != seq {
			return nil, status.Errorf(codes.Internal, "replication out of order")
		}
	}

	log.Printf("Updated message: %d", updatedMessage.Id)

	// Broadcast to subscribers
	s.broadcastEvent(&api.MessageEvent{
		SequenceNumber: atomic.AddInt64(&s.sequenceCounter, 1),
		Op:             api.OpType_OP_UPDATE,
		Message:        updatedMessage,
		EventAt:        timestamppb.Now(),
	}, req.TopicId)

	return updatedMessage, nil
}

// DeleteMessage deletes an existing message
func (s *MessageBoardServer) DeleteMessage(ctx context.Context, req *api.DeleteMessageRequest) (*emptypb.Empty, error) {
	if s.nodeID != "head" {
		return nil, status.Errorf(codes.PermissionDenied, "writes only allowed on head")
	}
	// Get existing messages for the topic
	var messages []*api.Message
	if err := s.messageStorage.ReadMessages(req.TopicId, &messages); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read messages: %v", err)
	}

	// Find the message and verify ownership
	var messageToDelete *api.Message
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

	seq := atomic.AddInt64(&s.eventCounter, 1)

	if s.nextReplica != nil {
		repReq := &api.ReplicationRequest{
			Op:             api.OpType_OP_DELETE,
			Message:        messageToDelete,
			SequenceNumber: seq,
		}

		resp, err := s.nextReplica.ReplicateOperation(ctx, repReq)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "replication failed: %v", err)
		}

		if resp.AckSequenceNumber != seq {
			return nil, status.Errorf(codes.Internal, "replication out of order")
		}
	}

	log.Printf("Deleted message: %d", req.MessageId)

	// Broadcast to subscribers
	s.broadcastEvent(&api.MessageEvent{
		SequenceNumber: atomic.AddInt64(&s.sequenceCounter, 1),
		Op:             api.OpType_OP_DELETE,
		Message:        messageToDelete,
		EventAt:        timestamppb.Now(),
	}, req.TopicId)

	return &emptypb.Empty{}, nil
}

// LikeMessage adds a like to a message
func (s *MessageBoardServer) LikeMessage(ctx context.Context, req *api.LikeMessageRequest) (*api.Message, error) {
	if s.nodeID != "head" {
		return nil, status.Errorf(codes.PermissionDenied, "writes only allowed on head")
	}
	// Get existing messages for the topic
	var messages []*api.Message
	if err := s.messageStorage.ReadMessages(req.TopicId, &messages); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read messages: %v", err)
	}

	// Find the message
	var message *api.Message
	for _, msg := range messages {
		if msg.Id == req.MessageId {
			message = msg
			break
		}
	}

	if message == nil {
		return nil, status.Errorf(codes.NotFound, "message not found: %d", req.MessageId)
	}

	// Toggle like
	like := &api.Like{
		TopicId:   req.TopicId,
		MessageId: req.MessageId,
		UserId:    req.UserId,
	}

	var likedNow bool
	if err := s.likeStorage.ToggleLike(like, &likedNow); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to toggle like: %v", err)
	}

	// Get updated like count
	var likeCount int64
	if err := s.likeStorage.ReadLikes(req.MessageId, &likeCount); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read likes: %v", err)
	}

	// Update message with new like count
	updatedMessage := &api.Message{
		Id:        message.Id,
		TopicId:   message.TopicId,
		UserId:    message.UserId,
		Text:      message.Text,
		CreatedAt: message.CreatedAt,
		Likes:     int32(likeCount),
	}

	seq := atomic.AddInt64(&s.eventCounter, 1)

	if s.nextReplica != nil {
		repReq := &api.ReplicationRequest{
			Op:             api.OpType_OP_LIKE,
			Message:        updatedMessage,
			SequenceNumber: seq,
		}

		resp, err := s.nextReplica.ReplicateOperation(ctx, repReq)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "replication failed: %v", err)
		}

		if resp.AckSequenceNumber != seq {
			return nil, status.Errorf(codes.Internal, "replication out of order")
		}
	}

	log.Printf("Liked message: %d (now %d likes)", req.MessageId, likeCount)

	// Broadcast to subscribers
	s.broadcastEvent(&api.MessageEvent{
		SequenceNumber: atomic.AddInt64(&s.sequenceCounter, 1),
		Op:             api.OpType_OP_LIKE,
		Message:        updatedMessage,
		EventAt:        timestamppb.Now(),
	}, req.TopicId)

	return updatedMessage, nil
}

// GetSubscriptionNode returns a node to which a subscription can be opened
func (s *MessageBoardServer) GetSubscriptionNode(ctx context.Context, req *api.SubscriptionNodeRequest) (*api.SubscriptionNodeResponse, error) {
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

	// Determine which node should handle this subscription
	targetNode := s.assignSubscriptionNode(ctx, req.UserId)

	return &api.SubscriptionNodeResponse{
		SubscribeToken: token,
		Node:           targetNode,
	}, nil
}

// assignSubscriptionNode calculates which node should handle a subscription for a given user
func (s *MessageBoardServer) assignSubscriptionNode(ctx context.Context, userID int64) *api.NodeInfo {
	// If no control plane, default to this node
	if s.controlPlaneClient == nil {
		return &api.NodeInfo{
			NodeId:  s.nodeID,
			Address: s.address,
		}
	}

	// Query control plane for node assignments
	resp, err := s.controlPlaneClient.GetSubscriptionResponsibility(ctx, &emptypb.Empty{})
	if err != nil {
		log.Printf("Failed to get subscription assignments: %v (using cached assignments)", err)
		// Use cached assignments (may be stale)
		s.responsibilityLock.RLock()
		staleness := time.Since(s.lastResponsibilityUpdate)
		if staleness > 60*time.Second {
			log.Printf("Warning: cached assignments stale for %.0fs", staleness.Seconds())
		}
		s.responsibilityLock.RUnlock()
		
		// Fallback to this node if no cached info
		return &api.NodeInfo{
			NodeId:  s.nodeID,
			Address: s.address,
		}
	}

	totalNodes := int32(len(resp.Assignments))
	if totalNodes == 0 {
		// No nodes registered, use this one
		return &api.NodeInfo{
			NodeId:  s.nodeID,
			Address: s.address,
		}
	}

	// Calculate target modulo index
	targetIndex := userID % int64(totalNodes)

	// Find node with matching modulo index
	for _, assignment := range resp.Assignments {
		if int64(assignment.ModuloIndex) == targetIndex {
			log.Printf("Assigned user %d to node %s (index %d of %d)", userID, assignment.Node.NodeId, targetIndex, totalNodes)
			return assignment.Node
		}
	}

	// Fallback (shouldn't happen if control plane is working correctly)
	log.Printf("Warning: no node found for user %d, falling back to self", userID)
	return &api.NodeInfo{
		NodeId:  s.nodeID,
		Address: s.address,
	}
}

// ListTopics returns all topics
func (s *MessageBoardServer) ListTopics(ctx context.Context, req *emptypb.Empty) (*api.ListTopicsResponse, error) {
	if s.nodeID != "tail" {
		return nil, status.Errorf(codes.PermissionDenied, "reads only allowed on tail")
	}
	var topics []*api.Topic
	if err := s.topicStorage.ReadTopics(&topics); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read topics: %v", err)
	}

	return &api.ListTopicsResponse{
		Topics: topics,
	}, nil
}

// ListSubscriptions returns subscriptions for a user
func (s *MessageBoardServer) ListSubscriptions(ctx context.Context, req *api.ListSubscriptionsRequest) (*api.ListSubscriptionsResponse, error) {
	if s.nodeID != "tail" {
		return nil, status.Errorf(codes.PermissionDenied, "reads only allowed on tail")
	}
	resp := &api.ListSubscriptionsResponse{}
	resp.TopicId = make([]int64, 0)
	if req == nil {
		return resp, fmt.Errorf("missing request")
	}
	if req.UserId == 0 {
		return resp, fmt.Errorf("missing user id")
	}
	if err := s.subscriptionStorage.ReadSubscriptions(req.UserId, &resp.TopicId); err != nil {
		return resp, err
	}
	return resp, nil
}

// GetMessages returns messages from a topic
func (s *MessageBoardServer) GetMessages(ctx context.Context, req *api.GetMessagesRequest) (*api.GetMessagesResponse, error) {
	if s.nodeID != "tail" {
		return nil, status.Errorf(codes.PermissionDenied, "reads only allowed on tail")
	}
	var messages []*api.Message
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
	var filteredMessages []*api.Message
	for _, msg := range messages {
		if msg.Id >= req.FromMessageId {
			filteredMessages = append(filteredMessages, msg)
		}
	}

	// Apply limit
	if req.Limit > 0 && len(filteredMessages) > int(req.Limit) {
		filteredMessages = filteredMessages[:req.Limit]
	}

	return &api.GetMessagesResponse{
		Messages: filteredMessages,
	}, nil
}

// SubscribeTopic subscribes to topics and streams message events
func (s *MessageBoardServer) SubscribeTopic(req *api.SubscribeTopicRequest, stream api.MessageBoard_SubscribeTopicServer) error {
	ctx := stream.Context()

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

	log.Printf("Subscription stream started for user %d topics: %v", req.UserId, req.TopicId)

	// Create subscription channel
	eventChan := make(chan *api.MessageEvent, 100)
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
			var messages []*api.Message
			if err := s.messageStorage.ReadMessages(topicID, &messages); err == nil {
				for _, msg := range messages {
					if msg.Id >= req.FromMessageId {
						// Update like count
						var likeCount int64
						s.likeStorage.ReadLikes(msg.Id, &likeCount)
						msg.Likes = int32(likeCount)

						event := &api.MessageEvent{
							SequenceNumber: atomic.AddInt64(&s.sequenceCounter, 1),
							Op:             api.OpType_OP_POST,
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
		log.Printf("Subscription stream closed for user %d topics: %v (reason: %v)", req.UserId, req.TopicId, ctx.Err())
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
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// broadcastEvent sends an event to all relevant subscribers
func (s *MessageBoardServer) broadcastEvent(event *api.MessageEvent, topicID int64) {
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
			// Check if this node is responsible for this user's broadcasts
			if !s.isResponsibleForUser(subInfo.UserID) {
				continue
			}

			select {
			case eventChan <- event:
			case <-time.After(100 * time.Millisecond):
				// Skip if channel is full
				log.Printf("Skipping event for slow subscriber")
			}
		}
	}
}

// isResponsibleForUser checks if this node should broadcast to this user
func (s *MessageBoardServer) isResponsibleForUser(userID int64) bool {
	s.responsibilityLock.RLock()
	defer s.responsibilityLock.RUnlock()

	// If no control plane or no nodes registered, broadcast to all (default behavior)
	if s.totalNodes == 0 {
		return true
	}

	// Warn if control plane is unavailable and assignments are getting stale
	if !s.controlPlaneAvailable && time.Since(s.lastResponsibilityUpdate) > 30*time.Second {
		log.Printf("Warning: control plane unavailable for %.0fs, using stale responsibility assignments", time.Since(s.lastResponsibilityUpdate).Seconds())
	}

	// Modulo-based responsibility: userID % totalNodes == myModuloIndex
	return (userID % int64(s.totalNodes)) == int64(s.myModuloIndex)
}

// generateToken generates a random subscription token
func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// createEventFromReplication creates a MessageEvent from a ReplicationRequest
func (s *MessageBoardServer) createEventFromReplication(req *api.ReplicationRequest) *api.MessageEvent {
	// Only message operations generate events for subscribers
	if req.Message == nil {
		return nil
	}

	return &api.MessageEvent{
		SequenceNumber: req.SequenceNumber,
		Op:             req.Op,
		Message:        req.Message,
		EventAt:        req.Message.CreatedAt,
	}
}

// extractTopicIDFromReplication extracts the topic ID from a ReplicationRequest
func (s *MessageBoardServer) extractTopicIDFromReplication(req *api.ReplicationRequest) int64 {
	if req.Topic != nil {
		return req.Topic.Id
	}
	if req.Message != nil {
		return req.Message.TopicId
	}
	return 0
}

// registerAndHeartbeat registers with control plane and sends periodic heartbeats
func (s *MessageBoardServer) registerAndHeartbeat(isHead, isTail bool) {
	if s.controlPlaneClient == nil {
		return
	}

	register := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := s.controlPlaneClient.RegisterNode(ctx, &api.RegisterNodeRequest{
			Node: &api.NodeInfo{
				NodeId:  s.nodeID,
				Address: s.address,
			},
			IsHead: isHead,
			IsTail: isTail,
		})

		if err != nil {
			log.Printf("Failed to register with control plane: %v", err)
		} else {
			log.Printf("Registered with control plane (head: %v, tail: %v)", isHead, isTail)
		}
	}

	// Initial registration
	register()

	// Sync responsibility immediately
	s.syncResponsibility()

	// Send heartbeat every 3 seconds
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		register()
		// Also sync responsibility to pick up cluster changes
		s.syncResponsibility()
	}
}

// syncResponsibility queries control plane for this node's subscription responsibility
func (s *MessageBoardServer) syncResponsibility() {
	if s.controlPlaneClient == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := s.controlPlaneClient.GetSubscriptionResponsibility(ctx, &emptypb.Empty{})

	if err != nil {
		s.responsibilityLock.Lock()
		s.controlPlaneAvailable = false
		elapsed := time.Since(s.lastResponsibilityUpdate)
		s.responsibilityLock.Unlock()

		log.Printf("Failed to sync responsibility (stale for %.1fs): %v", elapsed.Seconds(), err)
		return
	}

	// Find this node's assignment
	var myIndex int32 = -1
	var totalNodes int32 = 0

	for _, assignment := range resp.Assignments {
		if assignment.Node.NodeId == s.nodeID {
			myIndex = assignment.ModuloIndex
			totalNodes = assignment.TotalNodes
			break
		}
	}

	if myIndex >= 0 {
		s.responsibilityLock.Lock()
		s.myModuloIndex = myIndex
		s.totalNodes = totalNodes
		s.lastResponsibilityUpdate = time.Now()
		s.controlPlaneAvailable = true
		s.responsibilityLock.Unlock()

		log.Printf("Updated responsibility: myIndex=%d, totalNodes=%d (control plane available)", myIndex, totalNodes)
	} else {
		log.Printf("Node not found in responsibility assignments")
	}
}

func (s *MessageBoardServer) ReplicateOperation(ctx context.Context, req *api.ReplicationRequest) (*api.ReplicationResponse, error) {
	var ret struct{}
	now := time.Now().Format("15:04:05.000") // HH:MM:SS.mmm

	if req.User != nil {
		if err := s.userStorage.CreateUser(req.User, &ret); err != nil {
			return nil, status.Errorf(codes.Internal, "replicate create user failed: %v", err)
		}
		now = time.Now().Format("15:04:05.000")
		log.Printf("[%s] [Node %s] Replicated CreateUser: %d", now, s.nodeID, req.User.Id)
	} else if req.Topic != nil {
		if err := s.topicStorage.CreateTopic(req.Topic, &ret); err != nil {
			return nil, status.Errorf(codes.Internal, "replicate create topic failed: %v", err)
		}
		now := time.Now().Format("15:04:05.000")
		log.Printf("[%s] [Node %s] Replicated CreateTopic: %d", now, s.nodeID, req.Topic.Id)
	} else {
		switch req.Op {
		case api.OpType_OP_POST:
			if err := s.messageStorage.CreateMessage(req.Message, &ret); err != nil {
				return nil, status.Errorf(codes.Internal, "replicate post failed: %v", err)
			}
			now := time.Now().Format("15:04:05.000")
			log.Printf("[%s] [Node %s] Replicated PostMessage: %d", now, s.nodeID, req.Message.Id)

		case api.OpType_OP_UPDATE:
			if err := s.messageStorage.UpdateMessage(req.Message, &ret); err != nil {
				return nil, status.Errorf(codes.Internal, "replicate update failed: %v", err)
			}
			now := time.Now().Format("15:04:05.000")
			log.Printf("[%s] [Node %s] Replicated UpdateMessage: %d", now, s.nodeID, req.Message.Id)

		case api.OpType_OP_DELETE:
			if err := s.messageStorage.DeleteMessage(req.Message.Id, req.Message.TopicId, &ret); err != nil {
				return nil, status.Errorf(codes.Internal, "replicate delete failed: %v", err)
			}
			now := time.Now().Format("15:04:05.000")
			log.Printf("[%s] [Node %s] Replicated DeleteMessage: %d", now, s.nodeID, req.Message.Id)

		case api.OpType_OP_LIKE:
			var liked bool
			if err := s.likeStorage.ToggleLike(&api.Like{
				TopicId:   req.Message.TopicId,
				MessageId: req.Message.Id,
				UserId:    req.Message.UserId,
			}, &liked); err != nil {
				return nil, status.Errorf(codes.Internal, "replicate like failed: %v", err)
			}
			now := time.Now().Format("15:04:05.000")
			log.Printf("[%s] [Node %s] Replicated LikeMessage: %d (User %d)", now, s.nodeID, req.Message.Id, req.Message.UserId)
		}
	}

	atomic.StoreInt64(&s.sequenceCounter, req.SequenceNumber)

	// Forward to next node and wait for ACK
	var resp *api.ReplicationResponse
	var err error
	if s.nextReplica != nil {
		resp, err = s.nextReplica.ReplicateOperation(ctx, req)
		if err != nil {
			return nil, err
		}
		now = time.Now().Format("15:04:05.000")
		log.Printf("[%s] [Node %s] Received ack from next node: %d", now, s.nodeID, resp.AckSequenceNumber)
	} else {
		// Tail node: return ACK immediately
		now = time.Now().Format("15:04:05.000")
		log.Printf("[%s] [Node %s] Tail processed sequence %d, sending ack", now, s.nodeID, req.SequenceNumber)
		resp = &api.ReplicationResponse{AckSequenceNumber: req.SequenceNumber}
	}

	// ACK received (or we are tail) - broadcast to local subscribers
	event := s.createEventFromReplication(req)
	if event != nil {
		topicID := s.extractTopicIDFromReplication(req)
		if topicID > 0 {
			s.broadcastEvent(event, topicID)
			now = time.Now().Format("15:04:05.000")
			log.Printf("[%s] [Node %s] Broadcasted event to local subscribers (topic: %d)", now, s.nodeID, topicID)
		}
	}

	return resp, nil
}


// StartServer starts the gRPC server
func StartServer(id string, url string, nextAddress string, controlPlaneAddr string) {
	lis, err := net.Listen("tcp", url)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	server := NewMessageBoardServer(id, url, nextAddress)

	api.RegisterMessageBoardServer(grpcServer, server)
	api.RegisterReplicationServiceServer(grpcServer, server)

	log.Printf("Server listening on %s", url)

	if nextAddress != "" {
		conn, err := grpc.NewClient(nextAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("failed to connect to next node: %v", err)
		}
		server.nextReplica = api.NewReplicationServiceClient(conn)
	}

	// Connect to control plane if address provided
	if controlPlaneAddr != "" {
		conn, err := grpc.NewClient(controlPlaneAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("failed to connect to control plane: %v", err)
		}
		server.controlPlaneConn = conn
		server.controlPlaneClient = api.NewControlPlaneClient(conn)

		// Determine if we're head or tail
		isHead := (nextAddress != "")
		isTail := (nextAddress == "")

		// Register with control plane and start heartbeat
		go server.registerAndHeartbeat(isHead, isTail)
	}

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
