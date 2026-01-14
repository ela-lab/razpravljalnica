package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ela-lab/razpravljalnica/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ControlPlaneServer manages cluster state and subscription distribution
type ControlPlaneServer struct {
	api.UnimplementedControlPlaneServer

	mu sync.RWMutex

	// Node registry
	nodes          map[string]*NodeState // nodeID -> state
	headNodeID     string
	tailNodeID     string
	nodeOrder      []string // Ordered list of node IDs for consistent indexing
	lastNodeUpdate time.Time

	// Subscription round-robin counter
	subscriptionRRCounter int64

	// Subscription tracking: subscription ID -> node ID
	subscriptionAssignments map[string]string // "userID:topicID" -> nodeID
}

// NodeState tracks node info and health
type NodeState struct {
	Info                  *api.NodeInfo
	LastHeartbeat         time.Time
	IsHead                bool
	IsTail                bool
	AssignedSubscriptions []string // Track subscriptions assigned to this node
	ExpectedNextAddress   string   // What control plane thinks this node's next should be
	ExpectedPrevAddress   string   // What control plane thinks this node's prev should be
}

// chainString returns the current chain order as a readable string
func (cp *ControlPlaneServer) chainString() string {
	if len(cp.nodeOrder) == 0 {
		return "[]"
	}

	parts := make([]string, 0, len(cp.nodeOrder))
	for _, nodeID := range cp.nodeOrder {
		if state, ok := cp.nodes[nodeID]; ok && state.Info != nil {
			parts = append(parts, fmt.Sprintf("%s(%s)", nodeID, state.Info.Address))
		} else {
			parts = append(parts, nodeID)
		}
	}

	return strings.Join(parts, " -> ")
}

// NewControlPlaneServer creates a new control plane server
func NewControlPlaneServer() *ControlPlaneServer {
	cp := &ControlPlaneServer{
		nodes:                   make(map[string]*NodeState),
		nodeOrder:               []string{},
		subscriptionAssignments: make(map[string]string),
	}

	// Start background health checker
	go cp.healthCheckLoop()

	return cp
}

// RegisterNode handles node registration and heartbeat
func (cp *ControlPlaneServer) RegisterNode(ctx context.Context, req *api.RegisterNodeRequest) (*emptypb.Empty, error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	nodeID := req.Node.NodeId

	// Update or create node state
	state, exists := cp.nodes[nodeID]
	if !exists {
		state = &NodeState{
			Info:                  req.Node,
			AssignedSubscriptions: []string{},
		}
		cp.nodes[nodeID] = state
		cp.lastNodeUpdate = time.Now()
		log.Printf("Control plane: Node registered: %s", nodeID)

		// New node added - update topology with targeted notifications
		cp.addNodeToTopology(nodeID)
	} else {
		// Existing node heartbeat - check for topology drift
		// Only check nextAddress since servers don't track prevAddress
		if state.ExpectedNextAddress != req.CurrentNextAddress {
			log.Printf("Control plane: Topology drift detected for %s (expected next: %s, got: %s)",
				nodeID, state.ExpectedNextAddress, req.CurrentNextAddress)

			// Resend topology update asynchronously
			go cp.notifyNodeTopologyChange(
				nodeID,
				state.Info.Address,
				state.ExpectedNextAddress,
				state.ExpectedPrevAddress,
				state.IsHead,
				state.IsTail,
			)
		}
	}

	state.LastHeartbeat = time.Now()
	state.IsHead = req.IsHead
	state.IsTail = req.IsTail

	// Update head/tail tracking
	if req.IsHead {
		cp.headNodeID = nodeID
	}
	if req.IsTail {
		cp.tailNodeID = nodeID
	}

	return &emptypb.Empty{}, nil
}

// GetClusterState returns current cluster topology
func (cp *ControlPlaneServer) GetClusterState(ctx context.Context, req *emptypb.Empty) (*api.GetClusterStateResponse, error) {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	resp := &api.GetClusterStateResponse{
		AllNodes: make([]*api.NodeInfo, 0, len(cp.nodeOrder)),
	}

	// Set head and tail
	if headState, ok := cp.nodes[cp.headNodeID]; ok {
		resp.Head = headState.Info
	}
	if tailState, ok := cp.nodes[cp.tailNodeID]; ok {
		resp.Tail = tailState.Info
	}

	// Add all nodes in order
	for _, nodeID := range cp.nodeOrder {
		if state, ok := cp.nodes[nodeID]; ok {
			resp.AllNodes = append(resp.AllNodes, state.Info)
		}
	}

	return resp, nil
}

// GetSubscriptionResponsibility returns modulo-based subscription assignments
func (cp *ControlPlaneServer) GetSubscriptionResponsibility(ctx context.Context, req *emptypb.Empty) (*api.SubscriptionResponsibilityResponse, error) {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	resp := &api.SubscriptionResponsibilityResponse{
		Assignments: make([]*api.NodeResponsibilityAssignment, 0, len(cp.nodeOrder)),
	}

	totalNodes := int32(len(cp.nodeOrder))
	if totalNodes == 0 {
		return resp, nil
	}

	// Assign each node a modulo index based on its position in the ordered list
	for i, nodeID := range cp.nodeOrder {
		if state, ok := cp.nodes[nodeID]; ok {
			resp.Assignments = append(resp.Assignments, &api.NodeResponsibilityAssignment{
				Node:        state.Info,
				ModuloIndex: int32(i),
				TotalNodes:  totalNodes,
			})
		}
	}

	return resp, nil
}

// reconstructChainAndNotify rebuilds chain topology and notifies only affected nodes
func (cp *ControlPlaneServer) reconstructChainAndNotify(removedNodeID string) {
	log.Printf("Control plane: Reconstructing chain after %s removal", removedNodeID)

	// Remove the failed node from nodeOrder (maintain creation order)
	newOrder := make([]string, 0, len(cp.nodeOrder)-1)
	for _, nodeID := range cp.nodeOrder {
		if nodeID != removedNodeID {
			newOrder = append(newOrder, nodeID)
		}
	}
	cp.nodeOrder = newOrder

	if len(cp.nodeOrder) == 0 {
		log.Printf("Control plane: No nodes left in chain")
		cp.headNodeID = ""
		cp.tailNodeID = ""
		return
	}

	// Notify all nodes of their new positions
	for idx, nodeID := range cp.nodeOrder {
		nodeState := cp.nodes[nodeID]
		isHead := idx == 0
		isTail := idx == len(cp.nodeOrder)-1

		var prevAddr, nextAddr string
		if idx > 0 {
			prevAddr = cp.nodes[cp.nodeOrder[idx-1]].Info.Address
		}
		if idx < len(cp.nodeOrder)-1 {
			nextAddr = cp.nodes[cp.nodeOrder[idx+1]].Info.Address
		}

		// Check if node already had a prev address (was previously in the middle or was a tail)
		// Only newly added nodes should trigger catchup (they had empty prev before)
		oldPrevAddr := nodeState.ExpectedPrevAddress
		shouldTriggerCatchup := prevAddr != "" && oldPrevAddr == ""

		// Update expected state
		nodeState.ExpectedNextAddress = nextAddr
		nodeState.ExpectedPrevAddress = prevAddr

		// Update head/tail tracking
		if isHead {
			cp.headNodeID = nodeID
		}
		if isTail {
			cp.tailNodeID = nodeID
		}

		// Only send prevAddr if this node is newly getting a predecessor (newly added node)
		// Nodes that already had a prev don't need to catchup again
		prevAddrToSend := ""
		if shouldTriggerCatchup {
			prevAddrToSend = prevAddr
		}

		// Notify node of its new position
		go cp.notifyNodeTopologyChange(nodeID, nodeState.Info.Address, nextAddr, prevAddrToSend, isHead, isTail)
	}

	log.Printf("Control plane: Chain reconstructed with %d nodes (head: %s, tail: %s)",
		len(cp.nodeOrder), cp.headNodeID, cp.tailNodeID)
	log.Printf("Control plane: Chain order: %s", cp.chainString())
}

// addNodeToTopology inserts a new node and notifies only affected neighbors
func (cp *ControlPlaneServer) addNodeToTopology(newNodeID string) {
	// New node always becomes the tail (append to end)
	oldTailID := ""
	if len(cp.nodeOrder) > 0 {
		oldTailID = cp.nodeOrder[len(cp.nodeOrder)-1]
	}

	cp.nodeOrder = append(cp.nodeOrder, newNodeID)

	isHead := len(cp.nodeOrder) == 1
	isTail := true // New node is always the tail

	var prevNodeID, prevAddr string
	if !isHead {
		prevNodeID = oldTailID
		prevAddr = cp.nodes[prevNodeID].Info.Address
	}

	newNodeState := cp.nodes[newNodeID]
	newNodeState.ExpectedNextAddress = "" // Tail has no next
	newNodeState.ExpectedPrevAddress = prevAddr

	// Update head/tail tracking
	if isHead {
		cp.headNodeID = newNodeID
	}
	cp.tailNodeID = newNodeID

	// Notify the new tail node with prevAddr so it can catch up
	go cp.notifyNodeTopologyChange(newNodeID, newNodeState.Info.Address, "", prevAddr, isHead, isTail)

	// Notify old tail (now middle node) - its next changed to new node
	if prevNodeID != "" {
		oldTailState := cp.nodes[prevNodeID]
		oldTailIsHead := len(cp.nodeOrder) == 2
		oldTailIsTail := false // No longer tail

		// Old tail's prev stays the same, just update its next
		var oldTailPrevAddr string
		if len(cp.nodeOrder) > 2 {
			oldTailPrevAddr = cp.nodes[cp.nodeOrder[len(cp.nodeOrder)-3]].Info.Address
		}
		oldTailState.ExpectedPrevAddress = oldTailPrevAddr
		oldTailState.ExpectedNextAddress = newNodeState.Info.Address

		// Don't send prev in notification - old tail doesn't need to catch up
		go cp.notifyNodeTopologyChange(prevNodeID, oldTailState.Info.Address, newNodeState.Info.Address, "", oldTailIsHead, oldTailIsTail)
	}

	log.Printf("Control plane: Added node %s as new tail (position %d/%d, head: %v)",
		newNodeID, len(cp.nodeOrder)-1, len(cp.nodeOrder)-1, isHead)
	log.Printf("Control plane: Chain order: %s", cp.chainString())
}

// notifyNodeTopologyChange sends UpdateChainTopology RPC to a node
func (cp *ControlPlaneServer) notifyNodeTopologyChange(
	nodeID, nodeAddress, newNext, newPrev string,
	isHead, isTail bool,
) {

	// Connect to node
	conn, err := grpc.NewClient(nodeAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Control plane: Failed to connect to %s for topology update: %v", nodeID, err)
		return
	}
	defer conn.Close()

	client := api.NewReplicationServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &api.UpdateChainTopologyRequest{
		NewNextAddress:  newNext,
		NewPrevAddress:  newPrev,
		LastAckSequence: 0, // TODO: Track last ack sequence
		IsHead:          isHead,
		IsTail:          isTail,
	}

	_, err = client.UpdateChainTopology(ctx, req)
	if err != nil {
		log.Printf("Control plane: Failed to update topology on %s: %v", nodeID, err)
	}
}

// ReportNodeFailure allows nodes to report failures
func (cp *ControlPlaneServer) ReportNodeFailure(ctx context.Context, req *api.ReportNodeFailureRequest) (*emptypb.Empty, error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	log.Printf("Control plane: Node %s reports failure of %s", req.ReporterNodeId, req.FailedNodeId)

	// Check if failed node exists
	state, exists := cp.nodes[req.FailedNodeId]
	if exists {
		// Invalidate subscriptions assigned to this node
		for _, subID := range state.AssignedSubscriptions {
			log.Printf("Control plane: Invalidating subscription %s from failed node %s", subID, req.FailedNodeId)
			delete(cp.subscriptionAssignments, subID)
		}

		// Remove failed node
		delete(cp.nodes, req.FailedNodeId)

		// Reconstruct chain and notify affected nodes
		cp.reconstructChainAndNotify(req.FailedNodeId)
	}

	return &emptypb.Empty{}, nil
}

// AssignSubscriptionNode assigns a node for a subscription using round-robin
func (cp *ControlPlaneServer) AssignSubscriptionNode(ctx context.Context, req *api.AssignSubscriptionNodeRequest) (*api.AssignSubscriptionNodeResponse, error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if len(cp.nodeOrder) == 0 {
		return nil, fmt.Errorf("no nodes available in cluster")
	}

	// Create subscription identifier
	subID := fmt.Sprintf("%d:%d", req.UserId, req.TopicId)

	// Check if this subscription was already assigned - return the same assignment
	if nodeID, hadAssignment := cp.subscriptionAssignments[subID]; hadAssignment {
		nodeState, exists := cp.nodes[nodeID]
		if exists {
			log.Printf("Control plane: Returning existing assignment for subscription %s -> node %s", subID, nodeID)
			return &api.AssignSubscriptionNodeResponse{
				Node: nodeState.Info,
			}, nil
		}
		// If the assigned node no longer exists, fall through to assign a new one
		log.Printf("Control plane: Previous assignment for subscription %s to node %s no longer available, reassigning", subID, nodeID)
		delete(cp.subscriptionAssignments, subID)
	}

	// Assign to a new node using round-robin
	idx := atomic.AddInt64(&cp.subscriptionRRCounter, 1) - 1
	nodeIndex := idx % int64(len(cp.nodeOrder))

	nodeID := cp.nodeOrder[nodeIndex]
	nodeState := cp.nodes[nodeID]

	// Track assignment
	cp.subscriptionAssignments[subID] = nodeID
	nodeState.AssignedSubscriptions = append(nodeState.AssignedSubscriptions, subID)

	log.Printf("Control plane: Assigned subscription (user=%d, topic=%d) to node %s (index %d/%d)",
		req.UserId, req.TopicId, nodeID, nodeIndex, len(cp.nodeOrder))

	return &api.AssignSubscriptionNodeResponse{
		Node: nodeState.Info,
	}, nil
}

// healthCheckLoop periodically checks node health
func (cp *ControlPlaneServer) healthCheckLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		cp.mu.Lock()
		now := time.Now()
		timeout := 10 * time.Second

		// Check for stale nodes
		var staleNodes []string
		for nodeID, state := range cp.nodes {
			if now.Sub(state.LastHeartbeat) > timeout {
				staleNodes = append(staleNodes, nodeID)
			}
		}

		// Remove stale nodes and reconstruct chain
		for _, nodeID := range staleNodes {
			state := cp.nodes[nodeID]
			log.Printf("Control plane: Removing stale node %s (dead for %v)",
				nodeID, timeout)

			// Invalidate subscriptions assigned to this node
			for _, subID := range state.AssignedSubscriptions {
				log.Printf("Control plane: Invalidating subscription %s from failed node %s", subID, nodeID)
				delete(cp.subscriptionAssignments, subID)
			}

			// Remove node first, then reconstruct
			delete(cp.nodes, nodeID)
			cp.reconstructChainAndNotify(nodeID)
		}

		cp.mu.Unlock()
	}
}

// StartControlPlane starts the control plane gRPC server
func StartControlPlane(url string) {
	lis, err := net.Listen("tcp", url)
	if err != nil {
		log.Fatalf("Control plane failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	cp := NewControlPlaneServer()
	api.RegisterControlPlaneServer(grpcServer, cp)

	log.Printf("Control plane listening on %s", url)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Control plane failed to serve: %v", err)
	}
}
