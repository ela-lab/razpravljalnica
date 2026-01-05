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

## Project Structure

```
razpravljalnica/
├── api/                    # Protobuf definitions and generated code
├── internal/
│   ├── storage/           # Thread-safe in-memory storage
│   └── client/            # Reusable gRPC client service
├── cmd/
│   ├── server/            # Server executable
│   ├── cli/               # Command-line interface client
│   └── tui/               # Terminal UI client (tview)
├── tests/                  # Integration tests
├── Makefile               # Build automation
└── bin/                   # Compiled binaries (generated)
```

## Quick Start

### Building

```bash
# Build all binaries
make build

# Build specific components
make build-server
make build-cli
make build-tui
```

### Running the Server

```bash
./bin/razpravljalnica-server -p 9876
```

Or with make:
```bash
make dev-server
```

### Running Clients

**CLI Client:**
```bash
# Show help
./bin/razpravljalnica-cli -s localhost -p 9876 help

# Register a user
./bin/razpravljalnica-cli -s localhost -p 9876 register --name "Alice"

# Create a topic
./bin/razpravljalnica-cli -s localhost -p 9876 create-topic --title "General Discussion"

# Post a message
./bin/razpravljalnica-cli -s localhost -p 9876 post-message --userId 1 --topicId 1 --message "Hello!"

# List topics
./bin/razpravljalnica-cli -s localhost -p 9876 list-topics

# Subscribe to topics (real-time)
./bin/razpravljalnica-cli -s localhost -p 9876 subscribe --userId 1 --topicIds 1
```

**TUI Client (Terminal UI):**
```bash
./bin/razpravljalnica-tui -s localhost -p 9876
```

## Testing

```bash
make test                # Run all tests
make test-verbose        # Run with race detector
make test-coverage       # Generate coverage report
```

## Implementation Details

✅ Multi-user concurrent access  
✅ Thread-safe in-memory storage  
✅ Real-time subscriptions with streaming  
✅ Multiple client interfaces (CLI, TUI)
✅ Comprehensive unit tests
✅ Clean modular architecture
