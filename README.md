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

Razpravljalnica uses **chain replication** for fault tolerance and consistency:

- **Chain Replication**: Write operations flow from head through the chain to tail; reads available on all nodes (with dirty/clean filtering)
- **Dirty/Clean Markers**: Messages marked dirty on creation, cleaned after full replication ensures read consistency
- **Distributed Subscriptions**: Head node assigns subscriptions across chain using modulo-based hashing (`userID % nodeCount`)
- **Backward Broadcasting**: Events broadcast to subscribers as ACKs flow backward through the chain (after replication confirms)
- **No Control Plane**: Pure chain architecture without centralized coordinator

#### How Chain Replication Works

1. **Write Path**: Head receives write → replicates to next node → ... → tail confirms → ACKs flow back
2. **Read Consistency**: Messages marked dirty until full replication; reads filter dirty data
3. **Subscription Assignment**: Head node calculates `userID % nodeCount` to balance subscription load
4. **Event Broadcasting**: After replication completes, each node broadcasts to its assigned subscribers
5. **Node Roles**: 
   - Head: handles writes, assigns subscriptions
   - Middle: forwards replication, serves reads, broadcasts events
   - Tail: confirms replication, serves reads, broadcasts events

## Project Structure

```
razpravljalnica/
├── api/                    # Protobuf definitions and generated code
│   └── razpravljalnica.proto  # MessageBoard and ReplicationService
├── internal/
│   ├── storage/           # Thread-safe in-memory storage with dirty/clean tracking
│   └── client/            # Reusable gRPC client service
├── cmd/
│   ├── server/            # Chain replica node
│   ├── cli/               # Command-line interface client
│   └── tui/               # Terminal UI client (tview)
├── tests/                  # Integration tests (chain replication, dirty/clean markers)
├── Makefile               # Build automation
└── bin/                   # Compiled binaries (generated)
```

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

**Reads can be done on any node:**
```bash
./bin/razpravljalnica-cli -s localhost -p 9001 list-topics  # Read from head
./bin/razpravljalnica-cli -s localhost -p 9002 list-topics  # Read from tail
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
./bin/razpravljalnica-tui -s localhost -p 9001
```

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

✅ Multi-user concurrent access  
✅ Thread-safe in-memory storage  
✅ Real-time subscriptions with streaming  
✅ Chain replication (head writes, all nodes read)
✅ Dirty/clean markers for read consistency
✅ Distributed subscriptions with modulo-based load balancing
✅ Backward event broadcasting (post-ACK)
✅ Head-only subscription assignment (no control plane)
✅ Multiple client interfaces (CLI, TUI)
✅ Comprehensive unit and integration tests
✅ Clean modular architecture

## Architecture Highlights

### Chain Replication Flow

**Write Path:**
1. Client sends write to head node
2. Head creates message (marked dirty)
3. Head forwards to next node via `ReplicateOperation`
4. Middle node stores and forwards
5. Tail stores and returns ACK
6. ACKs flow backward through chain
7. Each node marks message clean upon receiving downstream ACK
8. Head returns success to client

**Read Path:**
- Reads allowed on all nodes
- `ReadMessages()` filters out dirty messages
- Ensures clients only see fully replicated data
- Tail always has clean data (end of chain)

### Subscription Assignment

**Head Node Responsibilities:**
- Only head processes `GetSubscriptionNode` requests
- Calculates `targetNode = userID % nodeCount`
- Generates subscription token
- Returns node assignment to client

**All Nodes:**
- Accept `SubscribeTopic` connections
- Store subscription tokens
- Broadcast events to their assigned subscribers
- Filter events based on user responsibility

### Dirty/Clean Consistency

**Storage Layer:**
- `dirtyMsgs map[int64]bool` tracks unreplicated messages
- `CreateMessage()` sets dirty flag
- `MarkMessageClean()` removes flag after replication
- `ReadMessages()` filters dirty data
- `IsMessageDirty()` checks replication status

**Replication Flow:**
- Head: marks clean after receiving ACK from downstream
- Middle: marks clean after receiving ACK from next node
- Tail: marks clean immediately (no downstream)

### Event Broadcasting

After replication completes (ACK received):
1. Tail broadcasts to local subscribers (if responsible)
2. ACK flows to middle node
3. Middle marks clean, broadcasts to local subscribers
4. ACK flows to head
5. Head marks clean, broadcasts to local subscribers
6. Client receives write confirmation

Each node broadcasts only to subscribers assigned to it based on modulo hashing.
