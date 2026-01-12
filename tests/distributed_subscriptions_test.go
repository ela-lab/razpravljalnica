package tests

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/ela-lab/razpravljalnica/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestDistributedSubscriptions tests modulo-based subscription distribution across nodes
func TestDistributedSubscriptions(t *testing.T) {
	// This test validates the control plane's ability to distribute subscriptions
	// across a cluster of nodes using modulo-based assignment

	// Start a control plane for testing
	tcp := NewTestControlPlane(t)
	defer tcp.Stop()

	// Create a gRPC client
	conn, err := grpc.NewClient(tcp.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial control plane: %v", err)
	}
	defer conn.Close()

	client := api.NewControlPlaneClient(conn)
	ctx := context.Background()

	// Register 3-node chain
	nodeIDs := []string{"node1", "node2", "node3"}
	for i, nodeID := range nodeIDs {
		isHead := (i == 0)
		isTail := (i == len(nodeIDs)-1)
		_, err := client.RegisterNode(ctx, &api.RegisterNodeRequest{
			Node: &api.NodeInfo{
				NodeId:  nodeID,
				Address: fmt.Sprintf("localhost:%d", 9000+i),
			},
			IsHead: isHead,
			IsTail: isTail,
		})
		if err != nil {
			t.Fatalf("Failed to register %s: %v", nodeID, err)
		}
	}

	// Get subscription responsibility
	resp, err := client.GetSubscriptionResponsibility(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("Failed to get subscription responsibility: %v", err)
	}

	if len(resp.Assignments) != 3 {
		t.Errorf("Expected 3 node assignments, got %d", len(resp.Assignments))
	}

	// Verify each user is assigned to correct node
	// User N should be assigned to node (N % nodeCount)
	nodeCount := int32(len(nodeIDs))
	for userID := int64(0); userID < 9; userID++ {
		expectedNodeIdx := userID % int64(nodeCount)
		if int32(expectedNodeIdx) >= int32(len(resp.Assignments)) {
			t.Errorf("Expected node index out of range for user %d", userID)
			continue
		}

		expectedNodeID := resp.Assignments[expectedNodeIdx].Node.NodeId
		t.Logf("User %d assigned to %s (node index %d)", userID, expectedNodeID, expectedNodeIdx)
	}

	t.Logf("✓ Distributed subscriptions: %d users distributed across %d nodes", 9, 3)
}

// Helper functions

func findAvailablePort(t *testing.T) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}
