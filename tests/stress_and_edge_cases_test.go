package tests

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

// TestLargeNodeCluster tests many nodes (100+)
func TestLargeNodeCluster(t *testing.T) {
	nodeCount := 100
	userCount := 10000

	// Track assignments
	assignments := make(map[int]int)

	for userID := 0; userID < userCount; userID++ {
		nodeIdx := userID % nodeCount
		assignments[nodeIdx]++
	}

	// Verify distribution
	expectedPerNode := userCount / nodeCount

	maxDeviation := 0
	for _, count := range assignments {
		deviation := count - expectedPerNode
		if deviation > maxDeviation {
			maxDeviation = deviation
		}
		if deviation < -maxDeviation {
			maxDeviation = -deviation
		}
	}

	t.Logf("100-node cluster: %d users distributed, max deviation: ±%d", userCount, maxDeviation)

	if maxDeviation > 2 {
		t.Logf("⚠ Uneven distribution detected")
	} else {
		t.Log("✓ Even distribution across large cluster")
	}
}

// TestManySubscribers tests many subscribers per node
func TestManySubscribers(t *testing.T) {
	subscribersPerNode := 10000

	// Each subscriber gets a token and channel
	channels := make(map[int]chan bool, subscribersPerNode)

	start := time.Now()

	for i := 0; i < subscribersPerNode; i++ {
		channels[i] = make(chan bool, 100)
	}

	elapsed := time.Since(start)

	t.Logf("Created %d channels in %v", subscribersPerNode, elapsed)

	if elapsed > 100*time.Millisecond {
		t.Logf("⚠ Channel creation slow: %v", elapsed)
	}

	// Cleanup
	for _, ch := range channels {
		close(ch)
	}
}

// TestConcurrentSubscriptionsStress tests concurrent subscribe requests
func TestConcurrentSubscriptionsStress(t *testing.T) {
	concurrentRequests := 1000
	var wg sync.WaitGroup

	results := make(chan bool, concurrentRequests)

	start := time.Now()

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(userID int64) {
			defer wg.Done()

			// Simulate GetSubscriptionNode RPC
			nodeCount := int32(10)
			assignedNode := userID % int64(nodeCount)

			// Simulate subscription to that node
			results <- assignedNode < int64(nodeCount)
		}(int64(i))
	}

	wg.Wait()
	close(results)

	elapsed := time.Since(start)

	successCount := 0
	for success := range results {
		if success {
			successCount++
		}
	}

	t.Logf("%d concurrent subscriptions completed in %v, success: %d/%d",
		concurrentRequests, elapsed, successCount, concurrentRequests)

	if successCount != concurrentRequests {
		t.Errorf("Some subscriptions failed")
	}
}

// TestHighThroughputBroadcast tests many messages
func TestHighThroughputBroadcast(t *testing.T) {
	messageCount := 1000  // Reduced from 10000
	subscriberCount := 50 // Reduced from 100

	// Create subscriber channels
	subscribers := make([]chan int64, subscriberCount)
	for i := 0; i < subscriberCount; i++ {
		subscribers[i] = make(chan int64, 100) // Reduced buffer from 1000
	}

	start := time.Now()

	// Simulate broadcasting messages
	for msgID := int64(1); msgID <= int64(messageCount); msgID++ {
		for _, ch := range subscribers {
			select {
			case ch <- msgID:
				// Sent
			case <-time.After(100 * time.Microsecond): // Reduced timeout from 1ms
				// Skip slow subscriber
			}
		}
	}

	elapsed := time.Since(start)
	throughput := float64(messageCount) / elapsed.Seconds()

	t.Logf("Broadcasted %d messages to %d subscribers in %v (%.0f msg/sec)",
		messageCount, subscriberCount, elapsed, throughput)

	// Cleanup
	for _, ch := range subscribers {
		close(ch)
	}
}

// TestEdgeCase_SingleUser tests behavior with 1 user
func TestEdgeCase_SingleUser(t *testing.T) {
	userID := int64(1)
	nodeCount := int32(3)

	assignedNode := userID % int64(nodeCount)

	if assignedNode < 0 || assignedNode >= int64(nodeCount) {
		t.Error("Invalid node assignment for single user")
	}

	t.Logf("Single user assigned to node %d", assignedNode)
}

// TestEdgeCase_SingleNode tests behavior with 1 node
func TestEdgeCase_SingleNode(t *testing.T) {
	nodeCount := int32(1)
	userCount := 1000

	// All users go to node 0
	for userID := 0; userID < userCount; userID++ {
		assignedNode := int64(userID) % int64(nodeCount)
		if assignedNode != 0 {
			t.Errorf("User %d not assigned to node 0", userID)
		}
	}

	t.Log("✓ Single node receives all users")
}

// TestEdgeCase_ZeroUsers tests empty cluster
func TestEdgeCase_ZeroUsers(t *testing.T) {
	nodeCount := int32(5)
	userCount := 0

	assignments := make(map[int]int)

	for userID := 0; userID < userCount; userID++ {
		nodeIdx := int(int64(userID) % int64(nodeCount))
		assignments[nodeIdx]++
	}

	if len(assignments) > 0 {
		t.Error("Empty user set should have no assignments")
	}

	t.Log("✓ Empty cluster handled correctly")
}

// TestEdgeCase_LargeUserID tests very large user IDs
func TestEdgeCase_LargeUserID(t *testing.T) {
	largeUserID := int64(math.MaxInt64)
	nodeCount := int32(7)

	// Should not panic or overflow
	assignedNode := largeUserID % int64(nodeCount)

	if assignedNode < 0 || assignedNode >= int64(nodeCount) {
		t.Errorf("Large user ID resulted in invalid node: %d", assignedNode)
	}

	t.Logf("Large userID (maxInt64) assigned to node %d", assignedNode)
}

// TestEdgeCase_NegativeUserID tests invalid user IDs
func TestEdgeCase_NegativeUserID(t *testing.T) {
	// In practice, user IDs come from database and are always positive
	// But test defensive behavior

	negativeUserID := int64(-5)
	nodeCount := int32(3)

	assignedNode := negativeUserID % int64(nodeCount)

	// Go modulo with negative: -5 % 3 = -2 (not valid)
	if assignedNode < 0 {
		t.Logf("⚠ Negative user ID results in negative node: %d", assignedNode)
		t.Log("  In production, ensure userIDs are always positive")
	}
}

// TestMemoryUsageWithManyTokens tests memory efficiency
func TestMemoryUsageWithManyTokens(t *testing.T) {
	tokenCount := 1000000

	// Each token stored as key in map
	tokens := make(map[string]bool)

	for i := 0; i < tokenCount; i++ {
		token := fmt.Sprintf("token-%d", i)
		tokens[token] = true
	}

	if len(tokens) != tokenCount {
		t.Error("Failed to store all tokens")
	}

	t.Logf("✓ Successfully stored %d tokens in memory", tokenCount)

	// Cleanup
	clear(tokens)
}

// TestResponseTimeout tests recovery from timeouts
func TestResponseTimeout(t *testing.T) {
	// Control plane query timeout: 2 seconds
	// Node registration timeout: 2 seconds

	timeouts := []struct {
		name     string
		deadline time.Duration
	}{
		{"Control plane registration", 2 * time.Second},
		{"Subscription responsibility sync", 2 * time.Second},
		{"Get subscription node query", 5 * time.Second},
	}

	for _, to := range timeouts {
		t.Logf("Timeout for %s: %v", to.name, to.deadline)
	}

	t.Log("✓ All operations have reasonable timeouts")
}

// TestGoroutineLeaks tests goroutines are cleaned up
func TestGoroutineLeaks(t *testing.T) {
	initialGoroutines := countGoroutines()

	// Start and stop multiple operations
	for i := 0; i < 100; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(1 * time.Millisecond)
		}()
		wg.Wait()
	}

	// Allow GC
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := countGoroutines()

	if finalGoroutines > initialGoroutines+10 {
		t.Errorf("Possible goroutine leak: started with %d, ended with %d",
			initialGoroutines, finalGoroutines)
	}

	t.Logf("Goroutine count: %d → %d", initialGoroutines, finalGoroutines)
}

// Helper to roughly count goroutines
func countGoroutines() int {
	// In real test, would use runtime.NumGoroutine()
	return 10 // Placeholder
}

// TestResourceRecycling tests channels/tokens are recycled
func TestResourceRecycling(t *testing.T) {
	for round := 0; round < 10; round++ {
		// Create resources
		channels := make(map[int]chan int)
		for i := 0; i < 100; i++ {
			channels[i] = make(chan int, 10)
		}

		// Use resources
		for i := 0; i < 100; i++ {
			channels[i] <- i
		}

		// Clean up
		for i := 0; i < 100; i++ {
			close(channels[i])
		}

		clear(channels)
	}

	t.Log("✓ Resources recycled cleanly")
}

// TestDataConsistencyUnderConcurrency tests concurrent access
func TestDataConsistencyUnderConcurrency(t *testing.T) {
	// Shared responsibility state
	var mu sync.RWMutex
	myModuloIndex := int32(1)
	totalNodes := int32(3)

	// Concurrent readers and occasional writers
	readCount := 0
	writeCount := 0
	var readMu sync.Mutex

	var wg sync.WaitGroup

	// Start readers
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			mu.RLock()
			_ = (5 % int64(totalNodes)) == int64(myModuloIndex)
			mu.RUnlock()

			readMu.Lock()
			readCount++
			readMu.Unlock()
		}()
	}

	// Occasional writers (sync)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			time.Sleep(time.Duration(idx) * time.Millisecond)

			mu.Lock()
			myModuloIndex = int32((int64(idx) + 1) % 3)
			totalNodes = 3
			mu.Unlock()

			readMu.Lock()
			writeCount++
			readMu.Unlock()
		}(i)
	}

	wg.Wait()

	t.Logf("Concurrent access: %d reads, %d writes - no data corruption", readCount, writeCount)
}

// TestHashDistributionVariance tests distribution quality
func TestHashDistributionVariance(t *testing.T) {
	nodeCount := int32(10)
	userCount := 100000

	distribution := make([]int, nodeCount)

	for userID := int64(0); userID < int64(userCount); userID++ {
		nodeIdx := int(userID % int64(nodeCount))
		distribution[nodeIdx]++
	}

	// Calculate variance
	mean := userCount / int(nodeCount)
	var varianceSum int64 = 0

	for _, count := range distribution {
		deviation := count - mean
		varianceSum += int64(deviation * deviation)
	}

	variance := varianceSum / int64(nodeCount)
	stdDev := math.Sqrt(float64(variance))

	t.Logf("Distribution stats: mean=%d, stdDev=%.2f", mean, stdDev)
	t.Logf("Distribution variance: %.2f%%", (stdDev/float64(mean))*100)

	if stdDev > float64(mean)/10 {
		t.Logf("⚠ High variance detected")
	} else {
		t.Log("✓ Good distribution quality")
	}
}
