# Integration Tests

This directory contains end-to-end integration tests for the Razpravljalnica discussion board service.

## Test Coverage

The integration tests verify the complete client-server interaction by starting a real server instance and testing all major operations:

### User Management
- **TestUserCreation**: Creates users and verifies ID assignment

### Topic Management
- **TestTopicCreation**: Creates topics and lists them
  
### Message Operations
- **TestMessageLifecycle**: Full lifecycle testing
  - Post message
  - Retrieve messages
  - Update message
  - Delete message

### Likes
- **TestLikeMessage**: Tests liking messages and like count updates

### Authorization
- **TestUnauthorizedOperations**: Verifies users can only modify their own messages

### Error Handling
- **TestNonExistentResources**: Tests operations on non-existent users/topics/messages

### Real-time Subscriptions
- **TestSubscription**: Tests real-time message subscription to a single topic
- **TestMultipleTopicSubscription**: Tests subscribing to multiple topics simultaneously

### Concurrency
- **TestConcurrentOperations**: Tests thread safety with 10 concurrent users posting messages

## Running Tests

```bash
# Run all tests (including integration tests)
make test

# Run only integration tests
go test -v ./tests/

# Run with race detector
go test -race -v ./tests/

# Run specific test
go test -v ./tests/ -run TestSubscription
```

## Test Implementation

The tests start a real server instance on a random available port for each test, ensuring isolation. Each test:

1. Starts a server process
2. Creates a client connection
3. Performs operations
4. Verifies results
5. Cleans up (stops server)

## Requirements

- Server binary must be built: `make build-server`
- Tests require network access to localhost
- Default timeout: 60 seconds per test suite

## Test Statistics

All 9 integration tests pass:
- Total execution time: ~6 seconds
- Tests per second: 1.5
- Server startup time per test: ~500ms
