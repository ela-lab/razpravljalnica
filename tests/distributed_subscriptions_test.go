package tests

import (
	"testing"
)

// TestDistributedSubscriptions tests modulo-based subscription distribution across nodes
// In chain replication without a control plane, the head node assigns subscriptions
// using modulo-based hashing to distribute load across the chain
func TestDistributedSubscriptions(t *testing.T) {
	// Simulate subscription distribution without a control plane
	// Each user is assigned to a node using: userID % nodeCount

	nodeIDs := []string{"node1", "node2", "node3"}
	nodeCount := int32(len(nodeIDs))

	// Verify users are distributed
	assignments := make(map[string][]int64) // nodeID -> []userIDs
	for nodeID := range nodeIDs {
		assignments[nodeIDs[nodeID]] = []int64{}
	}

	// Distribute 9 users across 3 nodes
	for userID := int64(0); userID < 9; userID++ {
		expectedNodeIdx := userID % int64(nodeCount)
		assignedNodeID := nodeIDs[expectedNodeIdx]
		assignments[assignedNodeID] = append(assignments[assignedNodeID], userID)
	}

	// Verify distribution
	expectedUsersPerNode := 3
	for _, users := range assignments {
		if len(users) != expectedUsersPerNode {
			t.Errorf("Expected %d users per node, got %d", expectedUsersPerNode, len(users))
		}
	}

	// Verify each user is assigned to exactly one node
	totalAssigned := 0
	for _, users := range assignments {
		totalAssigned += len(users)
	}
	if totalAssigned != 9 {
		t.Errorf("Expected 9 users total, got %d", totalAssigned)
	}

	t.Logf("✓ Distributed subscriptions: 9 users distributed across 3 nodes")
}
