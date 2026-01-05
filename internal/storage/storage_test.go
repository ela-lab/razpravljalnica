package storage

import (
	"testing"

	"github.com/ela-lab/razpravljalnica/api"
)

func TestUserStorageCreateAndRead(t *testing.T) {
	us := NewUserStorage()

	// Create a user
	user := &api.User{Id: 1, Name: "Alice"}
	var ret struct{}

	err := us.CreateUser(user, &ret)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Read the user back
	var name string
	err = us.ReadUser(1, &name)
	if err != nil {
		t.Fatalf("ReadUser failed: %v", err)
	}

	if name != "Alice" {
		t.Errorf("Expected name 'Alice', got '%s'", name)
	}

	// Read non-existent user
	var name2 string
	err = us.ReadUser(999, &name2)
	if err != nil {
		t.Fatalf("ReadUser for non-existent user failed: %v", err)
	}
	if name2 != "" {
		t.Errorf("Expected empty name for non-existent user, got '%s'", name2)
	}
}

func TestTopicStorageCreateAndRead(t *testing.T) {
	ts := NewTopicStorage()

	// Create topics
	topic1 := &api.Topic{Id: 1, Name: "General"}
	topic2 := &api.Topic{Id: 2, Name: "Random"}
	var ret struct{}

	ts.CreateTopic(topic1, &ret)
	ts.CreateTopic(topic2, &ret)

	// Read all topics
	var topics []*api.Topic
	err := ts.ReadTopics(&topics)
	if err != nil {
		t.Fatalf("ReadTopics failed: %v", err)
	}

	if len(topics) != 2 {
		t.Errorf("Expected 2 topics, got %d", len(topics))
	}
}

func TestMessageStorageCRUD(t *testing.T) {
	ms := NewMessageStorage()
	var ret struct{}

	// Create a message
	msg := &api.Message{
		Id:      1,
		TopicId: 1,
		UserId:  1,
		Text:    "Hello, World!",
	}

	err := ms.CreateMessage(msg, &ret)
	if err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}

	// Read messages
	var messages []*api.Message
	err = ms.ReadMessages(1, &messages)
	if err != nil {
		t.Fatalf("ReadMessages failed: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messages))
	}

	if messages[0].Text != "Hello, World!" {
		t.Errorf("Expected 'Hello, World!', got '%s'", messages[0].Text)
	}

	// Update message
	msg.Text = "Hello, Updated!"
	err = ms.UpdateMessage(msg, &ret)
	if err != nil {
		t.Fatalf("UpdateMessage failed: %v", err)
	}

	// Verify update
	messages = nil
	ms.ReadMessages(1, &messages)
	if messages[0].Text != "Hello, Updated!" {
		t.Errorf("Expected 'Hello, Updated!', got '%s'", messages[0].Text)
	}

	// Delete message
	err = ms.DeleteMessage(1, 1, &ret)
	if err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	// Verify deletion
	messages = nil
	ms.ReadMessages(1, &messages)
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages after deletion, got %d", len(messages))
	}
}

func TestLikeStorage(t *testing.T) {
	ls := NewLikeStorage()
	var ret struct{}

	// Create likes
	like1 := &api.Like{MessageId: 1, UserId: 1}
	like2 := &api.Like{MessageId: 1, UserId: 2}

	ls.CreateLike(like1, &ret)
	ls.CreateLike(like2, &ret)

	// Read like count
	var count int64
	err := ls.ReadLikes(1, &count)
	if err != nil {
		t.Fatalf("ReadLikes failed: %v", err)
	}

	if count != 2 {
		t.Errorf("Expected 2 likes, got %d", count)
	}

	// Try to like again (should not increase count)
	ls.CreateLike(like1, &ret)
	ls.ReadLikes(1, &count)
	if count != 2 {
		t.Errorf("Expected like count to remain 2, got %d", count)
	}
}
