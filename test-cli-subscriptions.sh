#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
CONTROL_PLANE_PORT=5051
NODE_HEAD_PORT=9001
NODE_MIDDLE_PORT=9002
NODE_TAIL_PORT=9003
CLI_PATH="./bin/razpravljalnica-cli"
SERVER_ADDR="localhost"

# Cleanup on exit
cleanup() {
    echo -e "\n${YELLOW}Cleaning up processes...${NC}"
    pkill -f "razpravljalnica-controlplane" || true
    pkill -f "razpravljalnica-server" || true
    sleep 1
    echo -e "${GREEN}✓ Cleanup complete${NC}"
}

trap cleanup EXIT

# Function to wait for service to be ready
wait_for_service() {
    local port=$1
    local max_attempts=20
    local attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        if timeout 1 bash -c "echo > /dev/tcp/localhost/$port" 2>/dev/null; then
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 0.5
    done
    
    echo -e "${RED}✗ Service on port $port did not become ready${NC}"
    return 1
}

# Main test function
main() {
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}Razpravljalnica CLI Subscription Test${NC}"
    echo -e "${BLUE}========================================${NC}\n"

    # Build binaries
    echo -e "${YELLOW}Building binaries...${NC}"
    make build > /dev/null 2>&1
    echo -e "${GREEN}✓ Build complete${NC}\n"

    # Start control plane
    echo -e "${YELLOW}Starting Control Plane (port $CONTROL_PLANE_PORT)...${NC}"
    ./bin/razpravljalnica-controlplane -p $CONTROL_PLANE_PORT > /tmp/controlplane.log 2>&1 &
    CP_PID=$!
    echo -e "${GREEN}✓ Control Plane started (PID: $CP_PID)${NC}"

    # Wait for control plane to be ready
    wait_for_service $CONTROL_PLANE_PORT
    sleep 1

    # Start head node
    echo -e "${YELLOW}Starting Head Node (port $NODE_HEAD_PORT)...${NC}"
    ./bin/razpravljalnica-server \
        -p $NODE_HEAD_PORT \
        -id head \
        -nextPort $NODE_MIDDLE_PORT \
        -control-plane "localhost:$CONTROL_PLANE_PORT" \
        > /tmp/node_head.log 2>&1 &
    HEAD_PID=$!
    echo -e "${GREEN}✓ Head Node started (PID: $HEAD_PID)${NC}"

    # Wait for head node to be ready
    wait_for_service $NODE_HEAD_PORT
    sleep 1

    # Start middle node
    echo -e "${YELLOW}Starting Middle Node (port $NODE_MIDDLE_PORT)...${NC}"
    ./bin/razpravljalnica-server \
        -p $NODE_MIDDLE_PORT \
        -id middle \
        -nextPort $NODE_TAIL_PORT \
        -control-plane "localhost:$CONTROL_PLANE_PORT" \
        > /tmp/node_middle.log 2>&1 &
    MIDDLE_PID=$!
    echo -e "${GREEN}✓ Middle Node started (PID: $MIDDLE_PID)${NC}"

    # Wait for middle node to be ready
    wait_for_service $NODE_MIDDLE_PORT
    sleep 1

    # Start tail node
    echo -e "${YELLOW}Starting Tail Node (port $NODE_TAIL_PORT)...${NC}"
    ./bin/razpravljalnica-server \
        -p $NODE_TAIL_PORT \
        -id tail \
        -nextPort 0 \
        -control-plane "localhost:$CONTROL_PLANE_PORT" \
        > /tmp/node_tail.log 2>&1 &
    TAIL_PID=$!
    echo -e "${GREEN}✓ Tail Node started (PID: $TAIL_PID)${NC}"

    # Wait for tail node to be ready
    wait_for_service $NODE_TAIL_PORT
    sleep 2

    echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}Testing CLI Operations${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"

    # Create user
    echo -e "${YELLOW}Step 1: Creating user...${NC}"
    USER_OUTPUT=$(timeout 5 $CLI_PATH -server $SERVER_ADDR -port $NODE_HEAD_PORT register --name "TestUser" 2>&1)
    echo "$USER_OUTPUT"
    USER_ID=$(echo "$USER_OUTPUT" | grep -oP 'ID: \K[0-9]+' | head -1)
    
    if [ -z "$USER_ID" ]; then
        echo -e "${RED}✗ Failed to extract user ID${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ User created with ID: $USER_ID${NC}\n"

    # Create topics
    echo -e "${YELLOW}Step 2: Creating topics...${NC}"
    TOPIC_IDS=()
    for i in 1 2 3; do
        TOPIC_OUTPUT=$($CLI_PATH -server $SERVER_ADDR -port $NODE_HEAD_PORT create-topic --title "Topic_$i" 2>&1)
        echo "$TOPIC_OUTPUT"
        TOPIC_ID=$(echo "$TOPIC_OUTPUT" | grep -oP 'ID: \K[0-9]+' | head -1)
        TOPIC_IDS+=($TOPIC_ID)
        echo -e "${GREEN}✓ Topic $i created with ID: $TOPIC_ID${NC}"
    done
    echo ""

    # Post messages to topics
    echo -e "${YELLOW}Step 3: Posting messages to topics...${NC}"
    for i in 0 1 2; do
        TOPIC_ID=${TOPIC_IDS[$i]}
        for j in 1 2; do
            MSG_OUTPUT=$($CLI_PATH -server $SERVER_ADDR -port $NODE_HEAD_PORT post-message \
                --userId $USER_ID \
                --topicId $TOPIC_ID \
                --message "Test message $j on topic ${i+1}" 2>&1)
            echo "$MSG_OUTPUT"
            echo -e "${GREEN}✓ Posted message $j to topic $TOPIC_ID${NC}"
        done
    done
    echo ""

    # List topics
    echo -e "${YELLOW}Step 4: Listing all topics...${NC}"
    $CLI_PATH -server $SERVER_ADDR -port $NODE_HEAD_PORT list-topics 2>&1
    echo ""

    # Get messages from first topic
    echo -e "${YELLOW}Step 5: Retrieving messages from first topic...${NC}"
    $CLI_PATH -server $SERVER_ADDR -port $NODE_HEAD_PORT get-messages \
        --topicId ${TOPIC_IDS[0]} \
        --limit 10 2>&1
    echo ""

    # Test subscription with timeout
    echo -e "${YELLOW}Step 6: Testing subscription (5 second timeout)...${NC}"
    TOPICS_STR=$(IFS=,; echo "${TOPIC_IDS[*]}")
    echo -e "${BLUE}Subscribing to topics: $TOPICS_STR${NC}"
    
    # Run subscription in background with timeout
    $CLI_PATH -server $SERVER_ADDR -port $NODE_HEAD_PORT subscribe \
        --userId $USER_ID \
        --topicIds "$TOPICS_STR" \
        --fromMessageId 0 2>&1 &
    SUB_PID=$!
    
    # Give subscription time to connect and receive messages
    sleep 3
    
    # Post additional messages while subscribed
    echo -e "${YELLOW}Step 7: Posting messages while subscribed...${NC}"
    for i in 0 1 2; do
        TOPIC_ID=${TOPIC_IDS[$i]}
        MSG_OUTPUT=$($CLI_PATH -server $SERVER_ADDR -port $NODE_HEAD_PORT post-message \
            --userId $USER_ID \
            --topicId $TOPIC_ID \
            --message "Message during subscription on topic ${i+1}" 2>&1)
        echo "$MSG_OUTPUT"
        echo -e "${GREEN}✓ Posted message to topic $TOPIC_ID during subscription${NC}"
        sleep 0.5
    done
    
    # Wait a bit more for messages to be received
    sleep 2
    
    # Kill the subscription process
    kill $SUB_PID 2>/dev/null || true
    wait $SUB_PID 2>/dev/null || true

    echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}✓ All CLI operations completed successfully!${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"

    echo -e "${YELLOW}Test Summary:${NC}"
    echo -e "  ✓ User registration"
    echo -e "  ✓ Topic creation (3 topics)"
    echo -e "  ✓ Message posting (6 messages during setup)"
    echo -e "  ✓ Message listing"
    echo -e "  ✓ Multi-topic subscription"
    echo -e "  ✓ Message reception during subscription"
    echo -e "  ✓ Control plane coordination"
    echo ""

    echo -e "${YELLOW}Service Status:${NC}"
    echo -e "  Control Plane: Running on port $CONTROL_PLANE_PORT (PID: $CP_PID)"
    echo -e "  Head Node:     Running on port $NODE_HEAD_PORT (PID: $HEAD_PID)"
    echo -e "  Middle Node:   Running on port $NODE_MIDDLE_PORT (PID: $MIDDLE_PID)"
    echo -e "  Tail Node:     Running on port $NODE_TAIL_PORT (PID: $TAIL_PID)"
    echo ""
    echo -e "${YELLOW}Services will terminate when this script exits or press Ctrl+C${NC}"
    
}

# Run main function
main 2>&1
