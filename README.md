# Razpravljalnica

A distributed discussion board service implemented in Go using gRPC.

## Overview

Razpravljalnica is a discussion board that allows users to:
- Register as users
- Create discussion topics
- Post messages within topics
- Like messages
- Edit and delete their own messages
- Subscribe to topics and receive real-time message updates

## Architecture

The implementation currently consists of:
- **Single gRPC server** with in-memory storage
- **MessageBoard service** implementing all required operations
- **Storage layer** with thread-safe in-memory data structures
- **Subscription system** for real-time event streaming

## Building

```bash
cd grpc
go build -o ../razpravljalnica-server .
```

## Running the Server

```bash
./razpravljalnica-server -p 9876
```

The server will start listening on port 9876 (default).

## Running the Client

```bash
./razpravljalnica-server -s localhost -p 9876
```

## Implementation Details

### Storage

The storage layer uses thread-safe in-memory data structures:
- `UserStorage`: Stores user information
- `TopicStorage`: Stores discussion topics
- `MessageStorage`: Stores messages organized by topic
- `LikeStorage`: Tracks likes on messages
- `SubscriptionStorage`: Manages user topic subscriptions

All storage operations are protected by RWMutex locks for concurrent access.

### Server Operations

**User Management:**
- `CreateUser`: Register a new user with auto-generated ID

**Topic Management:**
- `CreateTopic`: Create a new discussion topic
- `ListTopics`: List all available topics

**Message Operations:**
- `PostMessage`: Post a message to a topic (requires valid user and topic)
- `UpdateMessage`: Edit message text (only by original author)
- `DeleteMessage`: Remove a message (only by original author)
- `GetMessages`: Retrieve messages from a topic with optional filtering
- `LikeMessage`: Add a like to a message

**Subscriptions:**
- `GetSubscriptionNode`: Request a subscription token
- `SubscribeTopic`: Stream real-time message events for subscribed topics

### Event Broadcasting

When messages are posted, updated, deleted, or liked, the server broadcasts events to all subscribed clients. Events include:
- Sequence number (monotonically increasing)
- Operation type (POST, UPDATE, DELETE, LIKE)
- Message data
- Timestamp

## Features

✅ Multi-user concurrent access  
✅ Thread-safe in-memory storage  
✅ Real-time subscriptions with streaming  
✅ Message ownership verification  
✅ Like tracking  
✅ Historical message replay for new subscribers  
