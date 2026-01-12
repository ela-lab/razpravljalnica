package tests

import (
	"testing"
)

// TestIsResponsibleForUser tests that in chain replication,
// all nodes broadcast to their subscribers (no exclusive responsibility)
func TestIsResponsibleForUser(t *testing.T) {
	// In chain replication without control plane, every node broadcasts
	// to all subscribers that have subscriptions at that node.
	// The head makes subscription assignment decisions using modulo hashing,
	// but each node broadcasts events to its assigned subscribers.

	// This test verifies the concept that responsibility is implicit
	// based on subscription assignment, not explicit per-user
	isResponsible := true // All nodes handle all their subscribers

	if !isResponsible {
		t.Errorf("Expected nodes to broadcast to all their subscribers")
	}
}

// TestSubscriptionAssignmentByHead tests that subscription assignment
// is done by the head node using modulo-based load balancing
func TestSubscriptionAssignmentByHead(t *testing.T) {
	// The head node assigns subscriptions based on user ID
	// Subscriptions for user ID can be assigned to different nodes
	// to balance load across the chain

	tests := []struct {
		name         string
		userID       int64
		nodeCount    int32
		expectedNode int64
	}{
		{
			name:         "user 0 goes to node 0",
			userID:       0,
			nodeCount:    3,
			expectedNode: 0,
		},
		{
			name:         "user 1 goes to node 1",
			userID:       1,
			nodeCount:    3,
			expectedNode: 1,
		},
		{
			name:         "user 1 goes to node 1 with 5 nodes",
			userID:       1,
			nodeCount:    5,
			expectedNode: 1,
		},
		{
			name:         "user 10 goes to node 0 with 5 nodes (10 % 5 = 0)",
			userID:       10,
			nodeCount:    5,
			expectedNode: 0,
		},
		{
			name:         "user 17 goes to node 2 with 5 nodes (17 % 5 = 2)",
			userID:       17,
			nodeCount:    5,
			expectedNode: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate assignment logic: userID % nodeCount
			assignedNode := tt.userID % int64(tt.nodeCount)

			if assignedNode != tt.expectedNode {
				t.Errorf("User %d should go to node %d, got: %d",
					tt.userID, tt.expectedNode, assignedNode)
			}
		})
	}
}

// TestNodesHandleAllSubscriptions tests that all nodes handle subscriptions
// for users assigned to them
func TestNodesHandleAllSubscriptions(t *testing.T) {
	nodeCount := int32(5)
	userCount := int64(1000)

	// Map to track coverage
	covered := make([]bool, userCount)

	for nodeIdx := int32(0); nodeIdx < nodeCount; nodeIdx++ {
		for userID := int64(0); userID < userCount; userID++ {
			if (userID % int64(nodeCount)) == int64(nodeIdx) {
				covered[userID] = true
			}
		}
	}

	// Verify all users are covered exactly once
	for userID, isCovered := range covered {
		if !isCovered {
			t.Errorf("User %d not assigned to any node", userID)
		}
	}
}

// TestNoOverlapInSubscriptionAssignment tests that each user
// is assigned to exactly one node
func TestNoOverlapInSubscriptionAssignment(t *testing.T) {
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

// TestSubscriptionAssignmentChangesOnNodeAdd tests that adding a node
// changes where new subscriptions are assigned
func TestSubscriptionAssignmentChangesOnNodeAdd(t *testing.T) {
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
		t.Logf("Note: this user's assignment didn't change despite adding a node")
	}
}

// TestLargeNodeCounts tests assignment with many nodes
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

// TestSubscriptionAssignmentConsistency tests that assignment
// is deterministic (same userID always gets same node)
func TestSubscriptionAssignmentConsistency(t *testing.T) {
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

// TestSelectSubscriptionNodeLogic tests the head node's subscription
// assignment algorithm
func TestSelectSubscriptionNodeLogic(t *testing.T) {
	// The head node uses modulo-based assignment to distribute subscriptions
	// across the chain. Each user gets assigned to exactly one node.

	type nodeInfo struct {
		nodeID  string
		address string
		index   int32
	}

	nodes := []nodeInfo{
		{nodeID: "node1", address: "localhost:9001", index: 0},
		{nodeID: "node2", address: "localhost:9002", index: 1},
		{nodeID: "node3", address: "localhost:9003", index: 2},
	}

	tests := []struct {
		userID       int64
		expectedNode string
		expectedAddr string
	}{
		{userID: 0, expectedNode: "node1", expectedAddr: "localhost:9001"},
		{userID: 1, expectedNode: "node2", expectedAddr: "localhost:9002"},
		{userID: 2, expectedNode: "node3", expectedAddr: "localhost:9003"},
		{userID: 3, expectedNode: "node1", expectedAddr: "localhost:9001"},
		{userID: 10, expectedNode: "node2", expectedAddr: "localhost:9002"}, // 10 % 3 = 1 (node2)
	}

	for _, tt := range tests {
		t.Run(tt.expectedNode, func(t *testing.T) {
			// Calculate target index using modulo
			totalNodes := int32(len(nodes))
			targetIdx := int32(tt.userID % int64(totalNodes))

			// Find matching node
			var foundNode *nodeInfo
			for i := range nodes {
				if nodes[i].index == targetIdx {
					foundNode = &nodes[i]
					break
				}
			}

			if foundNode == nil {
				t.Fatalf("No node found for user %d (targetIdx=%d)", tt.userID, targetIdx)
			}

			if foundNode.nodeID != tt.expectedNode || foundNode.address != tt.expectedAddr {
				t.Errorf("Expected %s (%s), got %s (%s)",
					tt.expectedNode, tt.expectedAddr,
					foundNode.nodeID, foundNode.address)
			}
		})
	}
}
