# Razpravljalnica

A distributed discussion board service implemented in Go using gRPC with chain replication and distributed subscriptions.

## Overview

Razpravljalnica is a discussion board that allows users to:
- Register as users
- Create discussion topics
- Post messages within topics
- Like messages
- Edit and delete their own messages
- Subscribe to topics and receive real-time message updates

### Distributed Architecture

Razpravljalnica supports **chain replication** with distributed subscriptions:

- **Chain Replication**: Write operations flow from head to tail; reads happen at tail; ensures consistency
- **Distributed Subscriptions**: Subscription load is distributed across nodes using modulo-based hashing (`userID % nodeCount`)
- **Control Plane**: Centralized coordination server manages node registry, health monitoring, and subscription responsibility assignment
- **Backward Broadcasting**: Events are broadcast to subscribers as ACKs flow backward through the chain (after data is committed)

#### How Distributed Subscriptions Work

1. **Registration**: Nodes register with control plane on startup, sending heartbeats every 3s
2. **Responsibility Assignment**: Control plane maintains ordered node list and assigns each node a modulo index
3. **Subscription Delegation**: Head calculates `userID % totalNodes` and directs clients to the responsible node
4. **Filtered Broadcasting**: Each node only broadcasts events to users it's responsible for (matching `userID % totalNodes == myModuloIndex`)
5. **Token Storage**: All nodes store all subscription tokens; filtering happens during broadcast

## Project Structure

```
razpravljalnica/
├── api/                    # Protobuf definitions and generated code
│   └── razpravljalnica.proto  # Includes ControlPlane service
├── internal/
│   ├── storage/           # Thread-safe in-memory storage
│   └── client/            # Reusable gRPC client service
├── cmd/
│   ├── server/            # Chain replica node
│   ├── controlplane/      # Control plane server
│   ├── cli/               # Command-line interface client
│   └── tui/               # Terminal UI client (tview)
├── tests/                  # Integration tests (including distributed subscriptions)
├── Makefile               # Build automation
└── bin/                   # Compiled binaries (generated)
```

## Quick Start

### Building

```bash
# Build all binaries (server, control plane, cli, tui)
make build

# Build specific components
make build-server
make build-controlplane
make build-cli
make build-tui
```

### Running a Single Server (Non-Distributed)

```bash
./bin/razpravljalnica-server -p 9876
```

### Running a Distributed Chain with Control Plane

**Start control plane:**
```bash
./bin/razpravljalnica-controlplane -p 8080
```

**Start 3-node chain:**
```bash
# Node 1 (head)
./bin/razpravljalnica-server -p 9001 -id node1 -next localhost:9002 --control-plane localhost:8080

# Node 2 (middle)
./bin/razpravljalnica-server -p 9002 -id node2 -prev localhost:9001 -next localhost:9003 --control-plane localhost:8080

# Node 3 (tail)
./bin/razpravljalnica-server -p 9003 -id node3 -prev localhost:9002 --control-plane localhost:8080
```

**Connect clients to the head** for write operations:
```bash
./bin/razpravljalnica-cli -s localhost -p 9001 register --name "Alice"
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
./bin/razpravljalnica-tui -s localhost -p 9876
```

## Testing

```bash
make test                # Run all tests
make test-verbose        # Run with race detector
make test-coverage       # Generate coverage report
```

**Distributed subscription test:**
```bash
cd tests
go test -v -run TestDistributedSubscriptions
```

## Implementation Details

✅ Multi-user concurrent access  
✅ Thread-safe in-memory storage  
✅ Real-time subscriptions with streaming  
✅ Chain replication (head writes, tail reads)
✅ Distributed subscriptions with modulo-based load balancing
✅ Control plane for cluster coordination
✅ Backward event broadcasting (post-ACK)
✅ Multiple client interfaces (CLI, TUI)
✅ Comprehensive unit and integration tests
✅ Clean modular architecture

## Architecture Highlights

### Control Plane
- Tracks registered nodes via heartbeat (3s interval)
- Maintains ordered node list for consistent modulo assignment
- Provides `GetSubscriptionResponsibility` RPC to query node assignments
- Removes stale nodes after 10s timeout

### Node Registration
- Nodes connect to control plane on startup with `--control-plane` flag
- Register as head/tail based on chain position
- Sync responsibility (myModuloIndex, totalNodes) every 3s

### Subscription Assignment
- Head's `GetSubscriptionNode` queries control plane for node list
- Calculates `targetIndex = userID % totalNodes`
- Returns node matching targetIndex to client
- Client connects to designated node for streaming

### Event Broadcasting
- Replication: head → middle → tail (forward)
- Broadcasting: tail ← middle ← head (backward, on ACK return)
- Each node filters by `isResponsibleForUser(userID)` before sending
- Fallback to broadcast-all if control plane unavailable
