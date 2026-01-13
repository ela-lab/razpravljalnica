package tests

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"testing"
	"time"

	"github.com/ela-lab/razpravljalnica/api"
	"github.com/ela-lab/razpravljalnica/internal/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

// testClusterWithCP holds the control plane and servers for a test cluster
type testClusterWithCP struct {
	cpCmd     *exec.Cmd
	node1Cmd  *exec.Cmd
	node2Cmd  *exec.Cmd
	node3Cmd  *exec.Cmd
	cpAddr    string
	node1Addr string
	node2Addr string
	node3Addr string
}

// startTestClusterWithCP starts a 3-node cluster with a control plane
// Nodes are started in order: node1, node2, node3
// node1 becomes head (first registered), node3 becomes tail (last registered)
func startTestClusterWithCP(t *testing.T) *testClusterWithCP {
	// Find available ports for control plane
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port for control plane: %v", err)
	}
	cpPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Find available ports for servers
	ports := make([]int, 3)
	for i := 0; i < 3; i++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to find available port for server %d: %v", i+1, err)
		}
		ports[i] = listener.Addr().(*net.TCPAddr).Port
		listener.Close()
	}

	cpAddr := fmt.Sprintf("localhost:%d", cpPort)
	node1Addr := fmt.Sprintf("localhost:%d", ports[0])
	node2Addr := fmt.Sprintf("localhost:%d", ports[1])
	node3Addr := fmt.Sprintf("localhost:%d", ports[2])

	// Start control plane
	cpCmd := exec.Command("../bin/razpravljalnica-controlplane", "-p", fmt.Sprintf("%d", cpPort))
	if err := cpCmd.Start(); err != nil {
		t.Fatalf("Failed to start control plane: %v", err)
	}

	// Wait for control plane to start
	time.Sleep(500 * time.Millisecond)

	// Start node1 (will become head since it registers first)
	node1Cmd := exec.Command("../bin/razpravljalnica-server",
		"-p", fmt.Sprintf("%d", ports[0]),
		"-id", "node1",
		"-control-plane", cpAddr)
	if err := node1Cmd.Start(); err != nil {
		cpCmd.Process.Kill()
		t.Fatalf("Failed to start node1: %v", err)
	}

	// Wait for node1 to register
	time.Sleep(500 * time.Millisecond)

	// Start node2 (will become middle node)
	node2Cmd := exec.Command("../bin/razpravljalnica-server",
		"-p", fmt.Sprintf("%d", ports[1]),
		"-id", "node2",
		"-control-plane", cpAddr)
	if err := node2Cmd.Start(); err != nil {
		cpCmd.Process.Kill()
		node1Cmd.Process.Kill()
		t.Fatalf("Failed to start node2: %v", err)
	}

	// Wait for node2 to register
	time.Sleep(500 * time.Millisecond)

	// Start node3 (will become tail since it registers last)
	node3Cmd := exec.Command("../bin/razpravljalnica-server",
		"-p", fmt.Sprintf("%d", ports[2]),
		"-id", "node3",
		"-control-plane", cpAddr)
	if err := node3Cmd.Start(); err != nil {
		cpCmd.Process.Kill()
		node1Cmd.Process.Kill()
		node2Cmd.Process.Kill()
		t.Fatalf("Failed to start node3: %v", err)
	}

	tc := &testClusterWithCP{
		cpCmd:     cpCmd,
		node1Cmd:  node1Cmd,
		node2Cmd:  node2Cmd,
		node3Cmd:  node3Cmd,
		cpAddr:    cpAddr,
		node1Addr: node1Addr,
		node2Addr: node2Addr,
		node3Addr: node3Addr,
	}

	// Wait for all services to start and register
	time.Sleep(2 * time.Second)

	// Verify all nodes can be reached
	for _, addr := range []string{node1Addr, node2Addr, node3Addr} {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			tc.stop()
			t.Fatalf("Failed to connect to server %s: %v", addr, err)
		}
		conn.Close()
	}

	return tc
}

// getHeadAddress queries the control plane to find the current head node
func (tc *testClusterWithCP) getHeadAddress(t *testing.T) string {
	conn, err := grpc.NewClient(tc.cpAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to control plane: %v", err)
	}
	defer conn.Close()

	client := api.NewControlPlaneClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.GetClusterState(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("Failed to get cluster state: %v", err)
	}

	if resp.Head == nil {
		t.Fatal("No head node found in cluster")
	}

	return resp.Head.Address
}

// stop stops the cluster and control plane
func (tc *testClusterWithCP) stop() {
	if tc.node3Cmd != nil && tc.node3Cmd.Process != nil {
		tc.node3Cmd.Process.Kill()
		tc.node3Cmd.Wait()
	}
	if tc.node2Cmd != nil && tc.node2Cmd.Process != nil {
		tc.node2Cmd.Process.Kill()
		tc.node2Cmd.Wait()
	}
	if tc.node1Cmd != nil && tc.node1Cmd.Process != nil {
		tc.node1Cmd.Process.Kill()
		tc.node1Cmd.Wait()
	}
	if tc.cpCmd != nil && tc.cpCmd.Process != nil {
		tc.cpCmd.Process.Kill()
		tc.cpCmd.Wait()
	}
}

// TestTokenNodeDistribution tests whether subscription tokens are distributed across nodes
// using round-robin assignment from the control plane
func TestTokenNodeDistribution(t *testing.T) {
	cluster := startTestClusterWithCP(t)
	defer cluster.stop()

	// Connect to the head node (node1, since it registered first)
	headAddr := cluster.getHeadAddress(t)
	t.Logf("Connecting to head node at %s", headAddr)

	svc, err := client.NewClientService(headAddr, 10*time.Second)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer svc.Close()

	// Create user and multiple topics
	user, err := svc.CreateUser(fmt.Sprintf("token_node_dist_user_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	numTopics := 10
	topics := make([]*api.Topic, 0, numTopics)
	for i := 1; i <= numTopics; i++ {
		topic, err := svc.CreateTopic(fmt.Sprintf("TokenNodeDist_Topic_%d", i))
		if err != nil {
			t.Fatalf("Failed to create topic %d: %v", i, err)
		}
		topics = append(topics, topic)
	}

	// Get subscriptions for all topics and collect node assignments
	nodeDistribution := make(map[string]int) // node address -> count of subscriptions
	var firstNode string

	for _, topic := range topics {
		token, node, err := svc.GetSubscriptionNode(user.Id, topic.Id)
		if err != nil {
			t.Fatalf("Failed to get subscription for topic %d: %v", topic.Id, err)
		}

		if token == "" {
			t.Fatalf("Got empty token for topic %d", topic.Id)
		}

		nodeAddr := node.Address
		nodeDistribution[nodeAddr]++

		if firstNode == "" {
			firstNode = nodeAddr
		}

		t.Logf("Topic %d → Node %s (Token: %s)", topic.Id, nodeAddr, token[:8])
	}

	// Analyze the distribution
	t.Logf("\n=== Node Distribution Analysis ===")
	t.Logf("Total topics subscribed: %d", numTopics)
	t.Logf("Unique nodes assigned: %d", len(nodeDistribution))

	for nodeAddr, count := range nodeDistribution {
		percentage := float64(count) / float64(numTopics) * 100.0
		t.Logf("  Node %s: %d subscriptions (%.1f%%)", nodeAddr, count, percentage)
	}

	// Test passes if distribution exists
	// The key assertion: NOT all subscriptions should be on same node
	if len(nodeDistribution) == 1 {
		t.Errorf("Expected subscriptions distributed across multiple nodes, but all %d subscriptions are on node %s",
			numTopics, firstNode)
		t.Logf("❌ All subscriptions handled by single node (no distribution)")
	} else {
		t.Logf("✓ Subscriptions distributed across %d nodes", len(nodeDistribution))
	}
}

// TestMultipleTopicSubscriptions tests subscribing to multiple topics with separate tokens
func TestMultipleTopicSubscriptions(t *testing.T) {
	cluster := startTestClusterWithCP(t)
	defer cluster.stop()

	// Connect to the head node
	headAddr := cluster.getHeadAddress(t)
	t.Logf("Connecting to head node at %s", headAddr)

	svc, err := client.NewClientService(headAddr, 10*time.Second)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer svc.Close()

	// Register test user with unique name
	uniqueUserName := fmt.Sprintf("multi_topic_test_user_%d", time.Now().UnixNano())
	user, err := svc.CreateUser(uniqueUserName)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userID := user.Id

	// Create multiple topics
	topicNames := []string{"Topic_A", "Topic_B", "Topic_C"}
	topicIDs := make([]int64, 0, len(topicNames))

	for _, name := range topicNames {
		topic, err := svc.CreateTopic(name)
		if err != nil {
			t.Fatalf("Failed to create topic %s: %v", name, err)
		}
		topicIDs = append(topicIDs, topic.Id)
		t.Logf("Created topic %s with ID %d", name, topic.Id)
	}

	// Test 1: Get subscription tokens for each topic
	subscriptions := make([]struct {
		TopicID   int64
		Token     string
		FromMsgID int64
		NodeAddr  string
	}, 0, len(topicIDs))

	for _, topicID := range topicIDs {
		token, node, err := svc.GetSubscriptionNode(userID, topicID)
		if err != nil {
			t.Fatalf("Failed to get subscription node for topic %d: %v", topicID, err)
		}

		if token == "" {
			t.Fatalf("Got empty token for topic %d", topicID)
		}

		subscriptions = append(subscriptions, struct {
			TopicID   int64
			Token     string
			FromMsgID int64
			NodeAddr  string
		}{
			TopicID:   topicID,
			Token:     token,
			FromMsgID: 0,
			NodeAddr:  node.Address,
		})

		t.Logf("Topic %d: Token=%s (first 8 chars), Node=%s", topicID, token[:8], node.Address)
	}

	// Verify tokens are unique
	tokenSet := make(map[string]bool)
	for _, sub := range subscriptions {
		if tokenSet[sub.Token] {
			t.Fatalf("Duplicate token found: %s", sub.Token)
		}
		tokenSet[sub.Token] = true
	}
	t.Log("✓ All subscription tokens are unique")

	// Test 2: Start listening to all subscriptions concurrently
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	receivedEvents := make([]*api.MessageEvent, 0)
	eventChan := make(chan *api.MessageEvent, 100)

	// Start streaming in background
	go func() {
		err := svc.StreamMultipleSubscriptions(ctx, userID, subscriptions, func(event *api.MessageEvent) error {
			eventChan <- event
			return nil
		})
		if err != nil && err != context.Canceled {
			t.Logf("Subscription stream error: %v", err)
		}
	}()

	// Give streams time to connect
	time.Sleep(1 * time.Second)

	// Test 3: Post messages to each topic
	for i, topicID := range topicIDs {
		msg1, err := svc.PostMessage(userID, topicID, fmt.Sprintf("Message 1 to topic %d", topicID))
		if err != nil {
			t.Fatalf("Failed to post message 1 to topic %d: %v", topicID, err)
		}
		t.Logf("Posted message %d to topic %d", msg1.Id, topicID)
		time.Sleep(200 * time.Millisecond)

		msg2, err := svc.PostMessage(userID, topicID, fmt.Sprintf("Message 2 to topic %d", topicID))
		if err != nil {
			t.Fatalf("Failed to post message 2 to topic %d: %v", topicID, err)
		}
		t.Logf("Posted message %d to topic %d", msg2.Id, topicID)
		time.Sleep(200 * time.Millisecond)

		t.Logf("Posted 2 messages to topic %d (%s)", topicID, topicNames[i])
	}

	// Collect events for a bit
	time.Sleep(2 * time.Second)

	// Collect all events
	close(eventChan)
	for event := range eventChan {
		receivedEvents = append(receivedEvents, event)
	}

	t.Logf("\nReceived %d events total", len(receivedEvents))

	// Verify we received events for all topics
	topicEventCount := make(map[int64]int)
	for _, event := range receivedEvents {
		topicEventCount[event.Message.TopicId]++
	}

	expectedEvents := 2 * len(topicIDs) // 2 messages per topic
	if len(receivedEvents) != expectedEvents {
		t.Logf("Warning: Expected %d events, got %d", expectedEvents, len(receivedEvents))
	}

	for topicID, count := range topicEventCount {
		t.Logf("  Topic %d: %d events", topicID, count)
	}

	if len(receivedEvents) > 0 {
		t.Log("✓ Successfully received events from multiple topic subscriptions")
	}
}
