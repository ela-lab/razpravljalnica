# Razpravljalnica

A distributed discussion board service implemented in Go using gRPC with N-node chain replication, control plane coordination, distributed subscriptions with round-robin node assignment.

## Overview

Razpravljalnica is a discussion board that allows users to:
- Register as users
- Create discussion topics
- Post messages within topics
- Like messages
- Edit and delete their own messages
- Subscribe to multiple topics and receive real-time message updates

## Architecture

Razpravljalnica uses **chain replication** with a **control plane**:

### Core Design
- **Chain Replication**: Write operations flow head → middle nodes → tail; ACKs flow backward (ensures strong consistency)
- **Scalable Chain Length**: Supports any number of nodes (N ≥ 1)
- **Control Plane Coordination**: The control plane takes care of node additions and failures and subscription assignments

### Control Plane
- Tracks registered nodes via heartbeat
- Removes stale nodes if heartbeat fails
- Maintains ordered node list for subscription assignment
- Distributes subscriptions with a round-robin approach for optimal load balancing

### Node Topology
- **Head Node**: Accepts writes, forwards to next node
- **Middle Node(s)**: Replicates from previous, forwards to next, allows reading by subscriptions
- **Tail Node**: All one-time reads (GetUser, ListTopics, ListSubscriptions, GetMessages) are served here; sends ACKs backward through chain

### Subscription Mechanism
- Client calls `GetSubscriptionNode(userID, topicID)`
- Control plane assigns node via round-robin atomic counter
- Token registered only on assigned node
- Client connects to assigned node with token and subscribes

### Message Flow
```
Write (e.g., PostMessage):
  Client → Head → Replicate → Middle(s) → Replicate → Tail
  
  ACKs flow back to confirm writes: Tail → Middle(s) → Head

Reads (one-time operations):
  Client → Tail Node only
  ← Consistent results (committed at tail)

Subscribe (streaming events):
  Client → Designated Node (assigned by control plane)
  ← Real-time MessageEvents
```

### Event Broadcasting
- Replication: head → middle node(s) → tail through chain
- Filtering: Only to subscribers with registered tokens on that node
- Fallback: If control plane unavailable, broadcast uses existing token assignments

### Fault Tolerance
- **Detection**: Heartbeat monitoring removes failed nodes
- **Auto-recovery**: Client auto-reconnects (3 retries, 2s delay), resumes from last message ID
- **Reassignment**: Control plane caches assignments, reassigns if needed
- **Transparent**: No user intervention; no data loss

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

### Quick Cluster Startup (Example: 3-Node Chain + Control Plane)

**Automated startup script** - starts example 3-node cluster and keeps it running:
```bash
./start-cluster.sh
# Press Ctrl+C to stop all services
```

Services will be available on:
- Control Plane: `localhost:5051`
- Head Node: `localhost:9001`
- Middle Node: `localhost:9002`
- Tail Node: `localhost:9003`

### Running a Single Server (Non-Distributed)

```bash
./bin/razpravljalnica-server -p 9876
```

### Running a Distributed Chain with Control Plane (Manual)

**Start control plane:**
```bash
./bin/razpravljalnica-controlplane -p 5051
```

**Example: Start 3-node chain (head, middle, tail):**
```bash
# Node 1 (head) - writes enter here
./bin/razpravljalnica-server -p 9001 -id head -nextPort 9002 -control-plane localhost:5051

# Node 2 (middle) - replicates from head, reads are performed by subscriptions
./bin/razpravljalnica-server -p 9002 -id middle -nextPort 9003 -control-plane localhost:5051

# Node 3 (tail) - sends write ACKs back, reads are performed here
./bin/razpravljalnica-server -p 9003 -id tail -control-plane localhost:5051
```

**Note**: Any IDs can be used to distinguish the nodes. The tail node should be left with the `-nextPort` field blank.

All nodes should specify `-control-plane localhost:5051` to connect to the coordinator.

### Running Clients

**CLI Client:**
```bash
# Show help
./bin/razpravljalnica-cli -s localhost -p 9001 help

# Register a user
./bin/razpravljalnica-cli -s localhost -p 9001 register --name "Alice"

# Create a topic
./bin/razpravljalnica-cli -s localhost -p 9001 create-topic --title "General Discussion"

# Post a message
./bin/razpravljalnica-cli -s localhost -p 9001 post-message --userId 1 --topicId 1 --message "Hello!"

# List topics (tail-only)
./bin/razpravljalnica-cli -s localhost -p 9003 list-topics

# Subscribe to multiple topics (real-time streaming)
./bin/razpravljalnica-cli -s localhost -p 9001 subscribe --userId 1 --topicIds 1,2,3

# Get messages from a topic (tail-only)
./bin/razpravljalnica-cli -s localhost -p 9003 get-messages --topicId 1 --limit 10
```

Note:
- Writes (register user, create topic, post message) should be sent to the current head.
- One-time reads (get messages, list topics, list subscriptions, get user) must be sent to the current tail.
- Subscriptions connect to the node assigned by the control plane and stream events in real time.


**TUI Client (Terminal UI):**
```bash
./bin/razpravljalnica-tui -s localhost -p 9001
```

Inside the TUI:
- `F2` - Login/Register
- `F3` - Create new topic
- `S` - Toggle subscription to selected topic
- `Enter` - Like/unlike a message
- `E` - Edit your own message
- `Delete` - Delete your own message
- `F1` - Help
- `F12` - Exit

## Testing

```bash
make test                # Run all tests
```

### Test Scripts

**Test CLI with control plane cluster:**
```bash
./test-cli-subscriptions.sh
# Automated test that:
# - Starts control plane + 3-node cluster
# - Creates a user and topics
# - Posts messages
# - Tests multi-topic subscription
# - Verifies message reception
# - Cleans up all processes
```

**Automated cluster startup:**
```bash
./start-cluster.sh
# Keeps cluster running until Ctrl+C
# Services available for manual testing
```

### Integration Tests

```bash
# Run all integration tests
go test -v ./tests

# Test specific features
go test -v -run TestMultipleTopicSubscriptions ./tests
go test -v -run TestTokenNodeDistribution ./tests
go test -v -run TestCLISubscriptions ./tests
```

