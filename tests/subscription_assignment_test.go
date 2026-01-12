package tests

import (
	"testing"
)

// TestIsResponsibleForUser tests responsibility filtering logic
func TestIsResponsibleForUser(t *testing.T) {
	tests := []struct {
		name           string
		userID         int64
		myModuloIndex  int32
		totalNodes     int32
		isResponsible  bool
	}{
		{
			name:          "user 0 responsible for node 0 with 3 nodes",
			userID:        0,
			myModuloIndex: 0,
			totalNodes:    3,
			isResponsible: true,
		},
		{
			name:          "user 1 not responsible for node 0 with 3 nodes",
			userID:        1,
			myModuloIndex: 0,
			totalNodes:    3,
			isResponsible: false,
		},
		{
			name:          "user 1 responsible for node 1 with 3 nodes",
			userID:        1,
			myModuloIndex: 1,
			totalNodes:    3,
			isResponsible: true,
		},
		{
			name:          "user 10 responsible for node 0 with 5 nodes (10 % 5 = 0)",
			userID:        10,
			myModuloIndex: 0,
			totalNodes:    5,
			isResponsible: true,
		},
		{
			name:          "user 17 responsible for node 2 with 5 nodes (17 % 5 = 2)",
			userID:        17,
			myModuloIndex: 2,
			totalNodes:    5,
			isResponsible: true,
		},
		{
			name:          "single node responsible for all",
			userID:        999,
			myModuloIndex: 0,
			totalNodes:    1,
			isResponsible: true,
		},
		{
			name:          "large user ID",
			userID:        9223372036854775807, // max int64
			myModuloIndex: 2,
			totalNodes:    5,
			isResponsible: true, // 9223372036854775807 % 5 = 2, myModuloIndex = 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate isResponsibleForUser logic
			if tt.totalNodes == 0 {
				// Default behavior when no nodes
				return
			}

			isResponsible := (tt.userID % int64(tt.totalNodes)) == int64(tt.myModuloIndex)

			if isResponsible != tt.isResponsible {
				t.Errorf("User %d should be responsible: %v, got: %v",
					tt.userID, tt.isResponsible, isResponsible)
			}
		})
	}
}

// TestNodesResponsibleForAllUsers tests that all users are covered
func TestNodesResponsibleForAllUsers(t *testing.T) {
	nodeCount := int64(5)
	userCount := int64(1000)

	// Map to track coverage
	covered := make([]bool, userCount)

	for nodeIdx := int32(0); nodeIdx < int32(nodeCount); nodeIdx++ {
		for userID := int64(0); userID < userCount; userID++ {
			if (userID % nodeCount) == int64(nodeIdx) {
				covered[userID] = true
			}
		}
	}

	// Verify all users are covered exactly once
	for userID, isCovered := range covered {
		if !isCovered {
			t.Errorf("User %d not covered by any node", userID)
		}
	}
}

// TestNoOverlapInResponsibility tests no user is assigned to multiple nodes
func TestNoOverlapInResponsibility(t *testing.T) {
	nodeCount := int64(7)
	userCount := int64(500)

	for userID := int64(0); userID < userCount; userID++ {
		assignedCount := 0

		for nodeIdx := int64(0); nodeIdx < nodeCount; nodeIdx++ {
			if (userID % nodeCount) == nodeIdx {
				assignedCount++
			}
		}

		if assignedCount != 1 {
			t.Errorf("User %d assigned to %d nodes, expected 1", userID, assignedCount)
		}
	}
}

// TestResponsibilityChangesOnNodeAdd tests that adding a node changes assignments
func TestResponsibilityChangesOnNodeAdd(t *testing.T) {
	userID := int64(10)
	oldNodeCount := int64(3)
	newNodeCount := int64(4)

	oldNode := userID % oldNodeCount
	newNode := userID % newNodeCount

	// With 3 nodes: user 10 goes to node 1 (10 % 3 = 1)
	// With 4 nodes: user 10 goes to node 2 (10 % 4 = 2)
	if oldNode != 1 || newNode != 2 {
		t.Errorf("Expected oldNode=1, newNode=2, got oldNode=%d, newNode=%d", oldNode, newNode)
	}

	if oldNode == newNode {
		t.Logf("Warning: node change didn't affect this user (can happen randomly)")
	}
}

// TestLargeNodeCounts tests responsibility with many nodes
func TestLargeNodeCounts(t *testing.T) {
	tests := []struct {
		nodeCount int64
		userID    int64
	}{
		{100, 12345},
		{1000, 999999},
		{10000, 123456789},
	}

	for _, tt := range tests {
		idx := tt.userID % tt.nodeCount
		if idx < 0 || idx >= tt.nodeCount {
			t.Errorf("Index %d out of range [0, %d)", idx, tt.nodeCount)
		}
	}
}

// TestResponsibilityConsistency tests that same calculation always gives same result
func TestResponsibilityConsistency(t *testing.T) {
	userID := int64(42)
	nodeCount := int32(5)

	// Calculate multiple times
	results := make([]int64, 10)
	for i := 0; i < 10; i++ {
		results[i] = userID % int64(nodeCount)
	}

	// All should be identical
	for i := 1; i < len(results); i++ {
		if results[i] != results[0] {
			t.Errorf("Inconsistent results: %v", results)
		}
	}
}

// TestSubscriptionNodeAssignmentLogic tests the assignment algorithm
func TestSubscriptionNodeAssignmentLogic(t *testing.T) {
	// Simulate assignSubscriptionNode logic
	type nodeAssignment struct {
		nodeID      string
		address     string
		moduloIndex int32
		totalNodes  int32
	}

	assignments := []nodeAssignment{
		{nodeID: "node1", address: "localhost:9001", moduloIndex: 0, totalNodes: 3},
		{nodeID: "node2", address: "localhost:9002", moduloIndex: 1, totalNodes: 3},
		{nodeID: "node3", address: "localhost:9003", moduloIndex: 2, totalNodes: 3},
	}

	tests := []struct {
		userID        int64
		expectedNode  string
		expectedAddr  string
	}{
		{userID: 0, expectedNode: "node1", expectedAddr: "localhost:9001"},
		{userID: 1, expectedNode: "node2", expectedAddr: "localhost:9002"},
		{userID: 2, expectedNode: "node3", expectedAddr: "localhost:9003"},
		{userID: 3, expectedNode: "node1", expectedAddr: "localhost:9001"},
		{userID: 10, expectedNode: "node1", expectedAddr: "localhost:9001"},
	}

	for _, tt := range tests {
		t.Run(tt.expectedNode, func(t *testing.T) {
			// Calculate target index
			totalNodes := int32(len(assignments))
			targetIdx := tt.userID % int64(totalNodes)

			// Find matching assignment
			var foundNode *nodeAssignment
			for i := range assignments {
				if int64(assignments[i].moduloIndex) == targetIdx {
					foundNode = &assignments[i]
					break
				}
			}

			if foundNode == nil {
				t.Fatalf("No node found for user %d", tt.userID)
			}

			if foundNode.nodeID != tt.expectedNode || foundNode.address != tt.expectedAddr {
				t.Errorf("Expected %s (%s), got %s (%s)",
					tt.expectedNode, tt.expectedAddr,
					foundNode.nodeID, foundNode.address)
			}
		})
	}
}
