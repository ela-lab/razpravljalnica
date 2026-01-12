package main

import (
	"context"
	"log"
	"net"
	"sort"
	"sync"
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
}

// NodeState tracks node info and health
type NodeState struct {
	Info          *api.NodeInfo
	LastHeartbeat time.Time
	IsHead        bool
	IsTail        bool
}

// NewControlPlaneServer creates a new control plane server
func NewControlPlaneServer() *ControlPlaneServer {
	cp := &ControlPlaneServer{
		nodes:     make(map[string]*NodeState),
		nodeOrder: []string{},
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
			Info: req.Node,
		}
		cp.nodes[nodeID] = state
		cp.lastNodeUpdate = time.Now()
		log.Printf("Control plane: New node registered: %s (%s)", nodeID, req.Node.Address)
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

	// Rebuild node order when topology changes
	cp.rebuildNodeOrder()

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

// rebuildNodeOrder creates a consistent ordered list of nodes
func (cp *ControlPlaneServer) rebuildNodeOrder() {
	// Clear and rebuild
	cp.nodeOrder = make([]string, 0, len(cp.nodes))

	// Collect healthy node IDs
	for nodeID := range cp.nodes {
		cp.nodeOrder = append(cp.nodeOrder, nodeID)
	}

	// Sort for consistency
	sort.Strings(cp.nodeOrder)

	log.Printf("Control plane: Node order updated: %v (total: %d)", cp.nodeOrder, len(cp.nodeOrder))
}

// reconstructChain reconfigures chain topology after node failure
func (cp *ControlPlaneServer) reconstructChain(failedNodeID string) {
	log.Printf("Control plane: Reconstructing chain after %s failure", failedNodeID)
	
	// Find failed node's position in chain
	var predecessorID, successorID string
	
	// Build chain order before removal
	chainOrder := make([]string, 0, len(cp.nodeOrder))
	for _, nodeID := range cp.nodeOrder {
		if nodeID != failedNodeID {
			chainOrder = append(chainOrder, nodeID)
		}
	}
	
	if len(chainOrder) == 0 {
		log.Printf("Control plane: No nodes left in chain after %s failure", failedNodeID)
		return
	}
	
	// Find predecessor and successor
	for i, nodeID := range cp.nodeOrder {
		if nodeID == failedNodeID {
			if i > 0 {
				predecessorID = cp.nodeOrder[i-1]
			}
			if i < len(cp.nodeOrder)-1 {
				successorID = cp.nodeOrder[i+1]
			}
			break
		}
	}
	
	log.Printf("Control plane: Chain reconstruction - failed: %s, predecessor: %s, successor: %s",
		failedNodeID, predecessorID, successorID)
	
	// Handle different failure scenarios
	if predecessorID == "" && successorID == "" {
		// Only node in chain - nothing to do
		return
	} else if predecessorID == "" {
		// Head node failed - promote successor to head
		if successorState, ok := cp.nodes[successorID]; ok {
			cp.notifyNodeTopologyChange(successorID, successorState.Info.Address, "", "", true, false)
			cp.headNodeID = successorID
			log.Printf("Control plane: Promoted %s to head", successorID)
		}
	} else if successorID == "" {
		// Tail node failed - promote predecessor to tail
		if predecessorState, ok := cp.nodes[predecessorID]; ok {
			cp.notifyNodeTopologyChange(predecessorID, predecessorState.Info.Address, "", "", false, true)
			cp.tailNodeID = predecessorID
			log.Printf("Control plane: Promoted %s to tail", predecessorID)
		}
	} else {
		// Middle node failed - connect predecessor to successor
		predecessorState, okPred := cp.nodes[predecessorID]
		successorState, okSucc := cp.nodes[successorID]
		
		if okPred && okSucc {
			// Notify predecessor about new successor
			cp.notifyNodeTopologyChange(
				predecessorID,
				predecessorState.Info.Address,
				successorState.Info.Address,
				"",
				predecessorState.IsHead,
				false,
			)
			
			// Notify successor about new predecessor (for sync)
			cp.notifyNodeTopologyChange(
				successorID,
				successorState.Info.Address,
				"",
				predecessorState.Info.Address,
				false,
				successorState.IsTail,
			)
			
			log.Printf("Control plane: Connected %s -> %s (skipping failed %s)",
				predecessorID, successorID, failedNodeID)
		}
	}
}

// notifyNodeTopologyChange sends UpdateChainTopology RPC to a node
func (cp *ControlPlaneServer) notifyNodeTopologyChange(
	nodeID, nodeAddress, newNext, newPrev string,
	isHead, isTail bool,
) {
	log.Printf("Control plane: Notifying %s of topology change (next: %s, prev: %s, head: %v, tail: %v)",
		nodeID, newNext, newPrev, isHead, isTail)
	
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
		return
	}
	
	log.Printf("Control plane: Successfully updated topology on %s", nodeID)
}

// ReportNodeFailure allows nodes to report failures
func (cp *ControlPlaneServer) ReportNodeFailure(ctx context.Context, req *api.ReportNodeFailureRequest) (*emptypb.Empty, error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	
	log.Printf("Control plane: Node %s reports failure of %s", req.ReporterNodeId, req.FailedNodeId)
	
	// Check if failed node exists
	if _, exists := cp.nodes[req.FailedNodeId]; exists {
		// Reconstruct chain
		cp.reconstructChain(req.FailedNodeId)
		
		// Remove failed node
		delete(cp.nodes, req.FailedNodeId)
		cp.rebuildNodeOrder()
	}
	
	return &emptypb.Empty{}, nil
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
			log.Printf("Control plane: Removing stale node %s (dead for %v)",
				nodeID, timeout)
			
			// Reconstruct chain topology
			cp.reconstructChain(nodeID)
			
			delete(cp.nodes, nodeID)
			cp.rebuildNodeOrder()
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
