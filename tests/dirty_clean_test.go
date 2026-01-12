package tests

import (
	"testing"

	"github.com/ela-lab/razpravljalnica/api"
	"github.com/ela-lab/razpravljalnica/internal/storage"
)

func TestDirtyCleanMarkers(t *testing.T) {
	ms := storage.NewMessageStorage()
	var ret struct{}

	// Create a message (starts as dirty)
	msg := &api.Message{Id: 1, TopicId: 1, UserId: 1, Text: "Test message"}
	err := ms.CreateMessage(msg, &ret)
	if err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}

	// Verify message is dirty
	if !ms.IsMessageDirty(1) {
		t.Error("Expected message to be dirty after creation")
	}

	// Verify dirty messages are filtered from reads
	var messages []*api.Message
	err = ms.ReadMessages(1, &messages)
	if err != nil {
		t.Fatalf("ReadMessages failed: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages (dirty filtered), got %d", len(messages))
	}

	// Mark as clean
	err = ms.MarkMessageClean(1, &ret)
	if err != nil {
		t.Fatalf("MarkMessageClean failed: %v", err)
	}

	// Verify message is no longer dirty
	if ms.IsMessageDirty(1) {
		t.Error("Expected message to be clean after marking")
	}

	// Verify clean messages are visible in reads
	messages = nil
	err = ms.ReadMessages(1, &messages)
	if err != nil {
		t.Fatalf("ReadMessages failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 clean message, got %d", len(messages))
	}
	if len(messages) > 0 && messages[0].Text != "Test message" {
		t.Errorf("Expected message text 'Test message', got '%s'", messages[0].Text)
	}
}

func TestMultipleDirtyCleanMessages(t *testing.T) {
	ms := storage.NewMessageStorage()
	var ret struct{}

	// Create multiple messages
	for i := int64(1); i <= 3; i++ {
		msg := &api.Message{Id: i, TopicId: 1, UserId: 1, Text: "Message"}
		err := ms.CreateMessage(msg, &ret)
		if err != nil {
			t.Fatalf("CreateMessage %d failed: %v", i, err)
		}
	}

	// All should be dirty
	for i := int64(1); i <= 3; i++ {
		if !ms.IsMessageDirty(i) {
			t.Errorf("Expected message %d to be dirty", i)
		}
	}

	// No messages should be readable
	var messages []*api.Message
	err := ms.ReadMessages(1, &messages)
	if err != nil {
		t.Fatalf("ReadMessages failed: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages (all dirty), got %d", len(messages))
	}

	// Mark message 2 as clean
	err = ms.MarkMessageClean(2, &ret)
	if err != nil {
		t.Fatalf("MarkMessageClean failed: %v", err)
	}

	// Only message 2 should be readable
	messages = nil
	err = ms.ReadMessages(1, &messages)
	if err != nil {
		t.Fatalf("ReadMessages failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 clean message, got %d", len(messages))
	}
	if len(messages) > 0 && messages[0].Id != 2 {
		t.Errorf("Expected message ID 2, got %d", messages[0].Id)
	}

	// Mark all as clean
	for i := int64(1); i <= 3; i++ {
		err = ms.MarkMessageClean(i, &ret)
		if err != nil {
			t.Fatalf("MarkMessageClean %d failed: %v", i, err)
		}
	}

	// All messages should be readable now
	messages = nil
	err = ms.ReadMessages(1, &messages)
	if err != nil {
		t.Fatalf("ReadMessages failed: %v", err)
	}
	if len(messages) != 3 {
		t.Errorf("Expected 3 clean messages, got %d", len(messages))
	}
}

func TestReadAllMessagesIncludesDirty(t *testing.T) {
	ms := storage.NewMessageStorage()
	var ret struct{}

	// Create messages (all dirty)
	for i := int64(1); i <= 2; i++ {
		msg := &api.Message{Id: i, TopicId: 1, UserId: 1, Text: "Message"}
		err := ms.CreateMessage(msg, &ret)
		if err != nil {
			t.Fatalf("CreateMessage %d failed: %v", i, err)
		}
	}

	// ReadMessages should return 0 (dirty filtered)
	var cleanMessages []*api.Message
	err := ms.ReadMessages(1, &cleanMessages)
	if err != nil {
		t.Fatalf("ReadMessages failed: %v", err)
	}
	if len(cleanMessages) != 0 {
		t.Errorf("Expected 0 clean messages, got %d", len(cleanMessages))
	}

	// ReadAllMessages should return all (including dirty)
	var allMessages []*api.Message
	err = ms.ReadAllMessages(1, &allMessages)
	if err != nil {
		t.Fatalf("ReadAllMessages failed: %v", err)
	}
	if len(allMessages) != 2 {
		t.Errorf("Expected 2 total messages (including dirty), got %d", len(allMessages))
	}
}
