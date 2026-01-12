package tests

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/ela-lab/razpravljalnica/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestDistributedSubscriptions tests modulo-based subscription distribution across nodes
func TestDistributedSubscriptions(t *testing.T) {
	// Start control plane
	cpPort := findAvailablePort(t)
	cpAddress := fmt.Sprintf("localhost:%d", cpPort)
	cpCmd := exec.Command("../bin/razpravljalnica-controlplane", "-p", fmt.Sprintf("%d", cpPort))
	if err := cpCmd.Start(); err != nil {
		t.Fatalf("Failed to start control plane: %v", err)
	}
	defer cpCmd.Process.Kill()

	// Wait for control plane to start
	time.Sleep(500 * time.Millisecond)

	// Start 3-node chain with control plane
	node1Port := findAvailablePort(t)
	node2Port := findAvailablePort(t)
	node3Port := findAvailablePort(t)

	node1Addr := fmt.Sprintf("localhost:%d", node1Port)
	node2Addr := fmt.Sprintf("localhost:%d", node2Port)
	node3Addr := fmt.Sprintf("localhost:%d", node3Port)

	// Node 1 (head)
	node1Cmd := exec.Command("../bin/razpravljalnica-server",
		"-p", fmt.Sprintf("%d", node1Port),
		"-id", "node1",
		"-next", node2Addr,
		"--control-plane", cpAddress)
	if err := node1Cmd.Start(); err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1Cmd.Process.Kill()

	// Node 2 (middle)
	node2Cmd := exec.Command("../bin/razpravljalnica-server",
		"-p", fmt.Sprintf("%d", node2Port),
		"-id", "node2",
		"-prev", node1Addr,
		"-next", node3Addr,
		"--control-plane", cpAddress)
	if err := node2Cmd.Start(); err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2Cmd.Process.Kill()

	// Node 3 (tail)
	node3Cmd := exec.Command("../bin/razpravljalnica-server",
		"-p", fmt.Sprintf("%d", node3Port),
		"-id", "node3",
		"-prev", node2Addr,
		"--control-plane", cpAddress)
	if err := node3Cmd.Start(); err != nil {
		t.Fatalf("Failed to start node3: %v", err)
	}
	defer node3Cmd.Process.Kill()

	// Wait for nodes to register and stabilize
	time.Sleep(2 * time.Second)

	// Try connecting to head with retries
	var headConn *grpc.ClientConn
	var err error
	for i := 0; i < 10; i++ {
		headConn, err = grpc.Dial(node1Addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(), grpc.WithTimeout(1*time.Second))
		if err == nil {
			break
		}
		if i < 9 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if err != nil {
		t.Fatalf("Failed to connect to head after retries: %v", err)
	}
	defer headConn.Close()

	headClient := api.NewMessageBoardClient(headConn)

	// Create 3 users and a topic
	user1, err := headClient.CreateUser(context.Background(), &api.CreateUserRequest{Name: "Alice"})
	if err != nil {
		t.Fatalf("Failed to create user1: %v", err)
	}
	user2, err := headClient.CreateUser(context.Background(), &api.CreateUserRequest{Name: "Bob"})
	if err != nil {
		t.Fatalf("Failed to create user2: %v", err)
	}
	user3, err := headClient.CreateUser(context.Background(), &api.CreateUserRequest{Name: "Charlie"})
	if err != nil {
		t.Fatalf("Failed to create user3: %v", err)
	}

	topic, err := headClient.CreateTopic(context.Background(), &api.CreateTopicRequest{Name: "Distributed Test"})
	if err != nil {
		t.Fatalf("Failed to create topic: %v", err)
	}

	t.Logf("Created users: %d, %d, %d", user1.Id, user2.Id, user3.Id)
	t.Logf("Created topic: %d", topic.Id)

	// Get subscription nodes for each user
	// Each user should be assigned to a different node based on userID % 3
	subNode1, token1 := getSubscriptionNode(t, node1Addr, user1.Id, topic.Id)
	subNode2, token2 := getSubscriptionNode(t, node1Addr, user2.Id, topic.Id)
	subNode3, token3 := getSubscriptionNode(t, node1Addr, user3.Id, topic.Id)

	t.Logf("User %d assigned to node: %s", user1.Id, subNode1.NodeId)
	t.Logf("User %d assigned to node: %s", user2.Id, subNode2.NodeId)
	t.Logf("User %d assigned to node: %s", user3.Id, subNode3.NodeId)

	// Verify modulo distribution
	expectedNode1 := fmt.Sprintf("node%d", (user1.Id%3)+1)
	expectedNode2 := fmt.Sprintf("node%d", (user2.Id%3)+1)
	expectedNode3 := fmt.Sprintf("node%d", (user3.Id%3)+1)

	if subNode1.NodeId != expectedNode1 {
		t.Errorf("User %d expected node %s, got %s", user1.Id, expectedNode1, subNode1.NodeId)
	}
	if subNode2.NodeId != expectedNode2 {
		t.Errorf("User %d expected node %s, got %s", user2.Id, expectedNode2, subNode2.NodeId)
	}
	if subNode3.NodeId != expectedNode3 {
		t.Errorf("User %d expected node %s, got %s", user3.Id, expectedNode3, subNode3.NodeId)
	}

	// Connect each user to their assigned subscription node
	sub1 := subscribeToNode(t, subNode1.Address, token1, user1.Id, topic.Id)
	sub2 := subscribeToNode(t, subNode2.Address, token2, user2.Id, topic.Id)
	sub3 := subscribeToNode(t, subNode3.Address, token3, user3.Id, topic.Id)

	defer sub1.cancel()
	defer sub2.cancel()
	defer sub3.cancel()

	// Post messages via head and verify each user receives on their node
	var wg sync.WaitGroup
	wg.Add(3)

	received1 := make([]string, 0)
	received2 := make([]string, 0)
	received3 := make([]string, 0)

	// Start receiving
	go func() {
		defer wg.Done()
		for i := 0; i < 3; i++ {
			event, err := sub1.stream.Recv()
			if err != nil {
				t.Logf("User1 recv error: %v", err)
				return
			}
			received1 = append(received1, event.Message.Text)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 3; i++ {
			event, err := sub2.stream.Recv()
			if err != nil {
				t.Logf("User2 recv error: %v", err)
				return
			}
			received2 = append(received2, event.Message.Text)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 3; i++ {
			event, err := sub3.stream.Recv()
			if err != nil {
				t.Logf("User3 recv error: %v", err)
				return
			}
			received3 = append(received3, event.Message.Text)
		}
	}()

	// Give subscriptions time to establish
	time.Sleep(500 * time.Millisecond)

	// Post 3 messages
	_, err = headClient.PostMessage(context.Background(), &api.PostMessageRequest{
		TopicId: topic.Id,
		UserId:  user1.Id,
		Text:    "Message 1",
	})
	if err != nil {
		t.Fatalf("Failed to post message 1: %v", err)
	}

	_, err = headClient.PostMessage(context.Background(), &api.PostMessageRequest{
		TopicId: topic.Id,
		UserId:  user2.Id,
		Text:    "Message 2",
	})
	if err != nil {
		t.Fatalf("Failed to post message 2: %v", err)
	}

	_, err = headClient.PostMessage(context.Background(), &api.PostMessageRequest{
		TopicId: topic.Id,
		UserId:  user3.Id,
		Text:    "Message 3",
	})
	if err != nil {
		t.Fatalf("Failed to post message 3: %v", err)
	}

	// Wait for all subscriptions to receive
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for subscription events")
	}

	// Verify each user received all 3 messages
	if len(received1) != 3 {
		t.Errorf("User1 expected 3 messages, got %d: %v", len(received1), received1)
	}
	if len(received2) != 3 {
		t.Errorf("User2 expected 3 messages, got %d: %v", len(received2), received2)
	}
	if len(received3) != 3 {
		t.Errorf("User3 expected 3 messages, got %d: %v", len(received3), received3)
	}

	t.Logf("User1 received: %v", received1)
	t.Logf("User2 received: %v", received2)
	t.Logf("User3 received: %v", received3)
}

// Helper functions

func findAvailablePort(t *testing.T) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

func getSubscriptionNode(t *testing.T, headAddr string, userID, topicID int64) (*api.NodeInfo, string) {
	conn, err := grpc.Dial(headAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	mbClient := api.NewMessageBoardClient(conn)
	resp, err := mbClient.GetSubscriptionNode(context.Background(), &api.SubscriptionNodeRequest{
		UserId:  userID,
		TopicId: []int64{topicID},
	})
	if err != nil {
		t.Fatalf("Failed to get subscription node: %v", err)
	}

	return resp.Node, resp.SubscribeToken
}

type subscription struct {
	stream api.MessageBoard_SubscribeTopicClient
	cancel context.CancelFunc
}

func subscribeToNode(t *testing.T, nodeAddr, token string, userID, topicID int64) *subscription {
	conn, err := grpc.Dial(nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial subscription node: %v", err)
	}

	mbClient := api.NewMessageBoardClient(conn)
	ctx, cancel := context.WithCancel(context.Background())

	stream, err := mbClient.SubscribeTopic(ctx, &api.SubscribeTopicRequest{
		UserId:         userID,
		TopicId:        []int64{topicID},
		SubscribeToken: token,
		FromMessageId:  0,
	})
	if err != nil {
		cancel()
		t.Fatalf("Failed to subscribe: %v", err)
	}

	return &subscription{
		stream: stream,
		cancel: cancel,
	}
}
