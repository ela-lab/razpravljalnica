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
	"github.com/ela-lab/razpravljalnica/internal/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// testServer wraps the server process for testing
type testServer struct {
	headCmd  *exec.Cmd
	tailCmd  *exec.Cmd
	headAddr string
	tailAddr string
	headPort int
	tailPort int
}

// startTestServer starts a 2-node chain for integration testing (head + tail)
func startTestServer(t *testing.T) *testServer {
	// Find available ports
	listener1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	headPort := listener1.Addr().(*net.TCPAddr).Port
	listener1.Close()

	listener2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	tailPort := listener2.Addr().(*net.TCPAddr).Port
	listener2.Close()

	headAddr := fmt.Sprintf("localhost:%d", headPort)
	tailAddr := fmt.Sprintf("localhost:%d", tailPort)

	// Start tail node first (no next node)
	tailCmd := exec.Command("../bin/razpravljalnica-server", "-p", fmt.Sprintf("%d", tailPort), "-id", "tail")
	if err := tailCmd.Start(); err != nil {
		t.Fatalf("Failed to start tail node: %v", err)
	}

	// Start head node connected to tail
	headCmd := exec.Command("../bin/razpravljalnica-server", "-p", fmt.Sprintf("%d", headPort), "-id", "head", "-nextPort", fmt.Sprintf("%d", tailPort))
	if err := headCmd.Start(); err != nil {
		tailCmd.Process.Kill()
		t.Fatalf("Failed to start head node: %v", err)
	}

	ts := &testServer{
		headCmd:  headCmd,
		tailCmd:  tailCmd,
		headAddr: headAddr,
		tailAddr: tailAddr,
		headPort: headPort,
		tailPort: tailPort,
	}

	// Wait for both nodes to start and stabilize
	time.Sleep(1 * time.Second)

	// Verify head is running
	conn, err := grpc.NewClient(headAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		ts.stop()
		t.Fatalf("Failed to connect to head node: %v", err)
	}
	conn.Close()

	// Verify tail is running
	conn, err = grpc.NewClient(tailAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		ts.stop()
		t.Fatalf("Failed to connect to tail node: %v", err)
	}
	conn.Close()

	return ts
}

// stop stops both test servers
func (ts *testServer) stop() {
	if ts.headCmd != nil && ts.headCmd.Process != nil {
		ts.headCmd.Process.Kill()
		ts.headCmd.Wait()
	}
	if ts.tailCmd != nil && ts.tailCmd.Process != nil {
		ts.tailCmd.Process.Kill()
		ts.tailCmd.Wait()
	}
}

// TestUserCreation tests creating users
func TestUserCreation(t *testing.T) {
	srv := startTestServer(t)
	defer srv.stop()

	cs, err := client.NewClientService(srv.headAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cs.Close()

	// Create first user
	user1, err := cs.CreateUser("Alice")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	if user1.Name != "Alice" {
		t.Errorf("Expected user name 'Alice', got %q", user1.Name)
	}
	if user1.Id != 1 {
		t.Errorf("Expected first user ID to be 1, got %d", user1.Id)
	}

	// Create second user
	user2, err := cs.CreateUser("Bob")
	if err != nil {
		t.Fatalf("Failed to create second user: %v", err)
	}
	if user2.Id != 2 {
		t.Errorf("Expected second user ID to be 2, got %d", user2.Id)
	}
}

// TestTopicCreation tests creating topics
func TestTopicCreation(t *testing.T) {
	srv := startTestServer(t)
	defer srv.stop()

	cs, err := client.NewClientService(srv.headAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cs.Close()

	// Create topic
	topic, err := cs.CreateTopic("General Discussion")
	if err != nil {
		t.Fatalf("Failed to create topic: %v", err)
	}
	if topic.Name != "General Discussion" {
		t.Errorf("Expected topic name 'General Discussion', got %q", topic.Name)
	}
	if topic.Id != 1 {
		t.Errorf("Expected topic ID to be 1, got %d", topic.Id)
	}

	// List topics - reads allowed on all nodes
	topics, err := cs.ListTopics()
	if err != nil {
		t.Fatalf("Failed to list topics: %v", err)
	}
	if len(topics) != 1 {
		t.Errorf("Expected 1 topic, got %d", len(topics))
	}
}

// TestMessageLifecycle tests posting, updating, and deleting messages
func TestMessageLifecycle(t *testing.T) {
	srv := startTestServer(t)
	defer srv.stop()

	cs, err := client.NewClientService(srv.headAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cs.Close()

	// Create user and topic
	user, err := cs.CreateUser("Charlie")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	topic, err := cs.CreateTopic("Test Topic")
	if err != nil {
		t.Fatalf("Failed to create topic: %v", err)
	}

	// Post a message
	msg, err := cs.PostMessage(user.Id, topic.Id, "Hello, world!")
	if err != nil {
		t.Fatalf("Failed to post message: %v", err)
	}
	if msg.Text != "Hello, world!" {
		t.Errorf("Expected message text 'Hello, world!', got %q", msg.Text)
	}
	if msg.UserId != user.Id {
		t.Errorf("Expected message user ID %d, got %d", user.Id, msg.UserId)
	}
	if msg.TopicId != topic.Id {
		t.Errorf("Expected message topic ID %d, got %d", topic.Id, msg.TopicId)
	}

	// Get messages - reads allowed on all nodes
	messages, err := cs.GetMessages(topic.Id, 0, 10)
	if err != nil {
		t.Fatalf("Failed to get messages: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}

	// Update message
	updated, err := cs.UpdateMessage(user.Id, topic.Id, msg.Id, "Updated message")
	if err != nil {
		t.Fatalf("Failed to update message: %v", err)
	}
	if updated.Text != "Updated message" {
		t.Errorf("Expected updated text 'Updated message', got %q", updated.Text)
	}

	// Delete message
	err = cs.DeleteMessage(user.Id, topic.Id, msg.Id)
	if err != nil {
		t.Fatalf("Failed to delete message: %v", err)
	}

	// Verify message is deleted
	messages, err = cs.GetMessages(topic.Id, 0, 10)
	if err != nil {
		t.Fatalf("Failed to get messages after delete: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages after delete, got %d", len(messages))
	}
}

// TestLikeMessage tests liking messages
func TestLikeMessage(t *testing.T) {
	srv := startTestServer(t)
	defer srv.stop()

	cs, err := client.NewClientService(srv.headAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cs.Close()

	// Create users and topic
	user1, err := cs.CreateUser("Alice")
	if err != nil {
		t.Fatalf("Failed to create user1: %v", err)
	}
	user2, err := cs.CreateUser("Bob")
	if err != nil {
		t.Fatalf("Failed to create user2: %v", err)
	}
	topic, err := cs.CreateTopic("Test Topic")
	if err != nil {
		t.Fatalf("Failed to create topic: %v", err)
	}

	// Post a message
	msg, err := cs.PostMessage(user1.Id, topic.Id, "Like this message")
	if err != nil {
		t.Fatalf("Failed to post message: %v", err)
	}

	// Like message as user2
	liked, err := cs.LikeMessage(user2.Id, topic.Id, msg.Id)
	if err != nil {
		t.Fatalf("Failed to like message: %v", err)
	}
	if liked.Likes < 1 {
		t.Errorf("Expected at least 1 like, got %d", liked.Likes)
	}

	// Like again (should toggle off)
	liked2, err := cs.LikeMessage(user2.Id, topic.Id, msg.Id)
	if err != nil {
		t.Fatalf("Failed to like message second time: %v", err)
	}
	if liked2.Likes >= liked.Likes {
		t.Errorf("Expected likes to decrease on toggle, got %d from %d", liked2.Likes, liked.Likes)
	}
}

// TestUnauthorizedOperations tests that users can only modify their own messages
func TestUnauthorizedOperations(t *testing.T) {
	srv := startTestServer(t)
	defer srv.stop()

	cs, err := client.NewClientService(srv.headAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cs.Close()

	// Create two users
	user1, _ := cs.CreateUser("Alice")
	user2, _ := cs.CreateUser("Bob")
	topic, _ := cs.CreateTopic("Test Topic")

	// User1 posts a message
	msg, err := cs.PostMessage(user1.Id, topic.Id, "Alice's message")
	if err != nil {
		t.Fatalf("Failed to post message: %v", err)
	}

	// User2 tries to update user1's message
	_, err = cs.UpdateMessage(user2.Id, topic.Id, msg.Id, "Hacked!")
	if err == nil {
		t.Error("Expected error when user tries to update another user's message")
	} else {
		st, ok := status.FromError(err)
		if ok && st.Code() != codes.PermissionDenied {
			t.Logf("Got error code %v, expected PermissionDenied", st.Code())
		}
	}

	// User2 tries to delete user1's message
	err = cs.DeleteMessage(user2.Id, topic.Id, msg.Id)
	if err == nil {
		t.Error("Expected error when user tries to delete another user's message")
	} else {
		st, ok := status.FromError(err)
		if ok && st.Code() != codes.PermissionDenied {
			t.Logf("Got error code %v, expected PermissionDenied", st.Code())
		}
	}
}

// TestNonExistentResources tests operations on non-existent resources
func TestNonExistentResources(t *testing.T) {
	srv := startTestServer(t)
	defer srv.stop()

	cs, err := client.NewClientService(srv.headAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cs.Close()

	user, _ := cs.CreateUser("Alice")

	// Try to post to non-existent topic
	_, err = cs.PostMessage(user.Id, 999, "Message to nowhere")
	if err == nil {
		t.Error("Expected error when posting to non-existent topic")
	}

	// Try to get messages from non-existent topic (server returns empty list, not error)
	messages, err := cs.GetMessages(999, 0, 10)
	if err != nil {
		// Some implementations may return error, which is also acceptable
		t.Logf("Got error when getting messages from non-existent topic: %v", err)
	} else if len(messages) != 0 {
		t.Errorf("Expected empty messages list for non-existent topic, got %d messages", len(messages))
	}
}

// TestSubscription tests real-time subscription to topics
func TestSubscription(t *testing.T) {
	srv := startTestServer(t)
	defer srv.stop()

	cs, err := client.NewClientService(srv.headAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cs.Close()

	// Create user and topic
	user, _ := cs.CreateUser("Alice")
	topic, _ := cs.CreateTopic("Live Topic")

	// Get subscription token
	token, _, err := cs.GetSubscriptionNode(user.Id, []int64{topic.Id})
	if err != nil {
		t.Fatalf("Failed to get subscription token: %v", err)
	}
	if token == "" {
		t.Error("Expected non-empty subscription token")
	}

	// Start subscription in background
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events := make(chan *api.MessageEvent, 10)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		err := cs.StreamSubscription(ctx, user.Id, []int64{topic.Id}, token, 0, func(event *api.MessageEvent) error {
			select {
			case events <- event:
			default:
			}
			return nil
		})
		if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
			t.Logf("Subscription ended: %v", err)
		}
	}()

	// Wait for subscription to be established
	time.Sleep(500 * time.Millisecond)

	// Post a message
	msg, err := cs.PostMessage(user.Id, topic.Id, "Live message")
	if err != nil {
		t.Fatalf("Failed to post message: %v", err)
	}

	// Wait for event
	select {
	case event := <-events:
		if event.Message.Id != msg.Id {
			t.Errorf("Expected message ID %d, got %d", msg.Id, event.Message.Id)
		}
		if event.Message.Text != "Live message" {
			t.Errorf("Expected message text 'Live message', got %q", event.Message.Text)
		}
		t.Logf("Successfully received subscription event for message %d", event.Message.Id)
	case <-time.After(3 * time.Second):
		t.Error("Timeout waiting for subscription event")
	}

	cancel()
	wg.Wait()
}

// TestMultipleTopicSubscription tests subscribing to multiple topics
func TestMultipleTopicSubscription(t *testing.T) {
	srv := startTestServer(t)
	defer srv.stop()

	cs, err := client.NewClientService(srv.headAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cs.Close()

	// Create user and topics
	user, _ := cs.CreateUser("Alice")
	topic1, _ := cs.CreateTopic("Topic 1")
	topic2, _ := cs.CreateTopic("Topic 2")

	// Get subscription token for both topics
	token, _, err := cs.GetSubscriptionNode(user.Id, []int64{topic1.Id, topic2.Id})
	if err != nil {
		t.Fatalf("Failed to get subscription token: %v", err)
	}

	// Start subscription
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events := make(chan *api.MessageEvent, 10)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		cs.StreamSubscription(ctx, user.Id, []int64{topic1.Id, topic2.Id}, token, 0, func(event *api.MessageEvent) error {
			select {
			case events <- event:
			default:
			}
			return nil
		})
	}()

	time.Sleep(500 * time.Millisecond)

	// Post to both topics
	cs.PostMessage(user.Id, topic1.Id, "Message to topic 1")
	cs.PostMessage(user.Id, topic2.Id, "Message to topic 2")

	// Collect events
	receivedMessages := make(map[int64]string)
	timeout := time.After(3 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case event := <-events:
			receivedMessages[event.Message.TopicId] = event.Message.Text
			t.Logf("Received message for topic %d: %s", event.Message.TopicId, event.Message.Text)
		case <-timeout:
			t.Errorf("Timeout waiting for event %d of 2", i+1)
			break
		}
	}

	if len(receivedMessages) < 2 {
		t.Errorf("Expected 2 messages from different topics, got %d", len(receivedMessages))
	}

	cancel()
	wg.Wait()
}

// TestConcurrentOperations tests thread safety with multiple concurrent clients
func TestConcurrentOperations(t *testing.T) {
	srv := startTestServer(t)
	defer srv.stop()

	cs, err := client.NewClientService(srv.headAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cs.Close()

	// Create topic
	topic, _ := cs.CreateTopic("Concurrent Topic")

	// Create multiple users and post concurrently
	numUsers := 10
	var wg sync.WaitGroup
	wg.Add(numUsers)

	for i := 0; i < numUsers; i++ {
		go func(idx int) {
			defer wg.Done()
			user, err := cs.CreateUser(fmt.Sprintf("User%d", idx))
			if err != nil {
				t.Errorf("Failed to create user: %v", err)
				return
			}
			_, err = cs.PostMessage(user.Id, topic.Id, fmt.Sprintf("Message from User%d", idx))
			if err != nil {
				t.Errorf("Failed to post message: %v", err)
			}
		}(i)
	}

	wg.Wait()

	// Verify all messages were posted
	messages, err := cs.GetMessages(topic.Id, 0, 100)
	if err != nil {
		t.Fatalf("Failed to get messages: %v", err)
	}
	if len(messages) != numUsers {
		t.Errorf("Expected %d messages, got %d", numUsers, len(messages))
	}
}
