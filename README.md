# Razpravljalnica

**Razpravljalnica** is a distributed discussion board service implemented in Go, using gRPC and chain replication to ensure consistency, fault tolerance, and real-time message delivery.

## Features

- User registration  
- Topic creation and message posting  
- Message editing, deletion, and likes  
- Topic subscriptions with real-time updates  
- Command-line (CLI) and terminal UI (TUI) clients  

## Architecture Overview

The system is based on **chain replication**, providing strong consistency without relying on a centralized coordinator.

### Core Principles

- **Chain Replication**
  - Write operations flow from **head → tail**
  - Read operations are served by any node
- **Dirty/Clean Read Semantics**
  - Newly written data is marked *dirty* until fully replicated
  - Read operations filter out dirty data to guarantee consistency
- **Distributed Subscriptions**
  - Subscription ownership is deterministically assigned using modulo hashing
  - Each node manages and notifies its assigned subscribers
- **Backward Event Propagation**
  - Events are broadcast only after replication is confirmed

### Node Roles

- **Head node**
  - Accepts all write requests
  - Assigns subscription responsibility
- **Middle nodes**
  - Forward replication
  - Serve read requests
  - Notify local subscribers
- **Tail node**
  - Finalizes replication
  - Serves fully consistent reads

The architecture does not include a separate control plane or centralized coordinator.

## Project Structure

```text
razpravljalnica/
├── api/          # Protobuf definitions and generated code
├── internal/     # Storage and shared client logic
├── cmd/
│   ├── server/   # Chain replica node
│   ├── cli/      # Command-line client
│   └── tui/      # Terminal UI client
├── tests/        # Integration and consistency tests
├── Makefile      # Build automation
└── bin/          # Compiled binaries

## Quick Start

### Building

```bash
# Build all binaries (server, cli, tui)
make build

# Build specific components
make build-server
make build-cli
make build-tui
```

### Running a Single Server

```bash
./bin/razpravljalnica-server -id head -p 9876
```

### Running a Chain (2-node example)

**Start tail node first:**
```bash
# Tail node (no next node)
./bin/razpravljalnica-server -id tail -p 9002
```

**Start head node connected to tail:**
```bash
# Head node (points to tail via -nextPort)
./bin/razpravljalnica-server -id head -p 9001 -nextPort 9002
```

**Connect clients to the head** for write operations:
```bash
./bin/razpravljalnica-cli -s localhost -p 9001 register --name "Alice"
```

**Reads can be done on tail, writes on head:**
```bash
./bin/razpravljalnica-cli -s localhost -p 9002 list-topics  # Read from tail
./bin/razpravljalnica-cli -s localhost -p 9001 create-topic --title "Questions"  # Write to head
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

# Subscribe to topics (real-time) - automatically routed to appropriate node
./bin/razpravljalnica-cli -s localhost -p 9876 subscribe --userId 1 --topicIds 1
```

**TUI Client (Terminal UI):**
```bash
# Connect to head node
./bin/razpravljalnica-tui -s localhost -p 9001

# The TUI automatically:
# - Sends all writes to the head node
# - Gets subscription assignments from head
# - Connects to assigned nodes for streaming
```

**TUI Features:**
- F1: Help, F2: Login/Register, F3: Create Topic
- S: Subscribe/Unsubscribe to selected topic
- E: Edit your messages, Del: Delete your messages
- Enter: Like/Unlike messages, send messages
- Automatic routing to subscription nodes

## Testing

```bash
make test                # Run all tests
make test-verbose        # Run with race detector
make test-coverage       # Generate coverage report
```

**Key test suites:**
```bash
cd tests
go test -v -run TestDirtyCleanMarkers           # Dirty/clean consistency
go test -v -run TestDistributedSubscriptions    # Subscription load balancing
go test -v -run TestBackwardBroadcastFlow       # Event propagation
```

## Implementation Details

- Multi-user concurrent access  
- Thread-safe in-memory storage  
- Real-time subscriptions with streaming  
- Chain replication (head writes, tail reads)
- Distributed subscriptions with modulo-based load balancing
- Backward event broadcasting (post-ACK)
- Multiple client interfaces (CLI, TUI)
- Comprehensive unit and integration tests
- Clean modular architecture
