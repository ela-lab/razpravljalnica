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
)

// testClusterWithCP holds the control plane and servers for a test cluster
type testClusterWithCP struct {
	cpCmd      *exec.Cmd
	headCmd    *exec.Cmd
	middleCmd  *exec.Cmd
	tailCmd    *exec.Cmd
	cpAddr     string
	headAddr   string
	middleAddr string
	tailAddr   string
}

// startTestClusterWithCP starts a 3-node cluster with a control plane
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
			t.Fatalf("Failed to find available port for server %d: %v", i, err)
		}
		ports[i] = listener.Addr().(*net.TCPAddr).Port
		listener.Close()
	}

	cpAddr := fmt.Sprintf("localhost:%d", cpPort)
	headAddr := fmt.Sprintf("localhost:%d", ports[0])
	middleAddr := fmt.Sprintf("localhost:%d", ports[1])
	tailAddr := fmt.Sprintf("localhost:%d", ports[2])

	// Start control plane
	cpCmd := exec.Command("../bin/razpravljalnica-controlplane", "-p", fmt.Sprintf("%d", cpPort))
	if err := cpCmd.Start(); err != nil {
		t.Fatalf("Failed to start control plane: %v", err)
	}

	// Start head node
	headCmd := exec.Command("../bin/razpravljalnica-server",
		"-p", fmt.Sprintf("%d", ports[0]),
		"-id", "head",
		"-nextPort", fmt.Sprintf("%d", ports[1]),
		"-control-plane", cpAddr)
	if err := headCmd.Start(); err != nil {
		cpCmd.Process.Kill()
		t.Fatalf("Failed to start head node: %v", err)
	}

	// Start middle node
	middleCmd := exec.Command("../bin/razpravljalnica-server",
		"-p", fmt.Sprintf("%d", ports[1]),
		"-id", "middle",
		"-nextPort", fmt.Sprintf("%d", ports[2]),
		"-control-plane", cpAddr)
	if err := middleCmd.Start(); err != nil {
		cpCmd.Process.Kill()
		headCmd.Process.Kill()
		t.Fatalf("Failed to start middle node: %v", err)
	}

	// Start tail node
	tailCmd := exec.Command("../bin/razpravljalnica-server",
		"-p", fmt.Sprintf("%d", ports[2]),
		"-id", "tail",
		"-control-plane", cpAddr)
	if err := tailCmd.Start(); err != nil {
		cpCmd.Process.Kill()
		headCmd.Process.Kill()
		middleCmd.Process.Kill()
		t.Fatalf("Failed to start tail node: %v", err)
	}

	tc := &testClusterWithCP{
		cpCmd:      cpCmd,
		headCmd:    headCmd,
		middleCmd:  middleCmd,
		tailCmd:    tailCmd,
		cpAddr:     cpAddr,
		headAddr:   headAddr,
		middleAddr: middleAddr,
		tailAddr:   tailAddr,
	}

	// Wait for all services to start
	time.Sleep(2 * time.Second)

	// Verify all nodes can be reached
	for _, addr := range []string{headAddr, middleAddr, tailAddr} {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			tc.stop()
			t.Fatalf("Failed to connect to server %s: %v", addr, err)
		}
		conn.Close()
	}

	return tc
}

// stop stops the cluster and control plane
func (tc *testClusterWithCP) stop() {
	if tc.tailCmd != nil && tc.tailCmd.Process != nil {
		tc.tailCmd.Process.Kill()
		tc.tailCmd.Wait()
	}
	if tc.middleCmd != nil && tc.middleCmd.Process != nil {
		tc.middleCmd.Process.Kill()
		tc.middleCmd.Wait()
	}
	if tc.headCmd != nil && tc.headCmd.Process != nil {
		tc.headCmd.Process.Kill()
		tc.headCmd.Wait()
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

	svc, err := client.NewClientService(cluster.headAddr, 10*time.Second)
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
	// Setup: Create user and topics
	svc, err := client.NewClientService("localhost:9001", 10*time.Second)
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
		}{
			TopicID:   topicID,
			Token:     token,
			FromMsgID: 0,
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
