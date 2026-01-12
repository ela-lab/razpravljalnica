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

# PIDs array
declare -a PIDS

# Cleanup on exit
cleanup() {
    echo -e "\n${YELLOW}Shutting down cluster...${NC}"
    
    for pid in "${PIDS[@]}"; do
        if kill -0 $pid 2>/dev/null; then
            kill $pid 2>/dev/null || true
        fi
    done
    
    sleep 1
    
    # Force kill any remaining processes
    pkill -f "razpravljalnica-controlplane" || true
    pkill -f "razpravljalnica-server" || true
    
    echo -e "${GREEN}✓ Cluster shut down${NC}"
}

trap cleanup EXIT INT TERM

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

main() {
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}Starting Razpravljalnica Cluster${NC}"
    echo -e "${BLUE}========================================${NC}\n"

    # Build binaries
    echo -e "${YELLOW}Building binaries...${NC}"
    make build > /dev/null 2>&1
    echo -e "${GREEN}✓ Build complete${NC}\n"

    # Start control plane
    echo -e "${YELLOW}Starting Control Plane (port $CONTROL_PLANE_PORT)...${NC}"
    ./bin/razpravljalnica-controlplane -p $CONTROL_PLANE_PORT > /tmp/controlplane.log 2>&1 &
    CP_PID=$!
    PIDS+=($CP_PID)
    echo -e "${GREEN}✓ Control Plane started (PID: $CP_PID)${NC}"

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
    PIDS+=($HEAD_PID)
    echo -e "${GREEN}✓ Head Node started (PID: $HEAD_PID)${NC}"

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
    PIDS+=($MIDDLE_PID)
    echo -e "${GREEN}✓ Middle Node started (PID: $MIDDLE_PID)${NC}"

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
    PIDS+=($TAIL_PID)
    echo -e "${GREEN}✓ Tail Node started (PID: $TAIL_PID)${NC}"

    wait_for_service $NODE_TAIL_PORT
    sleep 1

    echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}✓ Cluster is running${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"

    echo -e "${YELLOW}Service Status:${NC}"
    echo -e "  Control Plane: Running on port $CONTROL_PLANE_PORT (PID: $CP_PID)"
    echo -e "  Head Node:     Running on port $NODE_HEAD_PORT (PID: $HEAD_PID)"
    echo -e "  Middle Node:   Running on port $NODE_MIDDLE_PORT (PID: $MIDDLE_PID)"
    echo -e "  Tail Node:     Running on port $NODE_TAIL_PORT (PID: $TAIL_PID)"
    echo ""
    echo -e "${YELLOW}Logs:${NC}"
    echo -e "  Control Plane: /tmp/controlplane.log"
    echo -e "  Head Node:     /tmp/node_head.log"
    echo -e "  Middle Node:   /tmp/node_middle.log"
    echo -e "  Tail Node:     /tmp/node_tail.log"
    echo ""
    echo -e "${BLUE}Press Ctrl+C to stop all services${NC}\n"

    # Wait for all processes
    wait
}

main
