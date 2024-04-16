package policy

import (
	"testing"
	"time"
)

func TestRoundRobinPolicy(t *testing.T) {
	p := NewRoundRobinPolicy()

	// Test SetReadyReplicas
	replicas := []string{"replica1", "replica2", "replica3"}
	p.SetReadyReplicas(replicas)

	// Test SelectReplica
	for i := 0; i < len(replicas); i++ {
		replica := p.SelectReplica(nil)
		if *replica != replicas[i] {
			t.Errorf("Expected replica %s, got %s", replicas[i], *replica)
		}
	}

	// Test round-robin behavior
	firstReplica := p.SelectReplica(nil)
	if *firstReplica != replicas[0] {
		t.Errorf("Expected first replica in the next round to be %s, got %s", replicas[0], *firstReplica)
	}

	// Test behavior when no replicas are available
	p.SetReadyReplicas([]string{})
	if p.SelectReplica(nil) != nil {
		t.Error("Expected nil when no replicas are available")
	}
}

func TestLeastNumberOfRequestsPolicy(t *testing.T) {
	p := NewLeastNumberOfRequestsPolicy()

	// Test SetReadyReplicas
	replicas := []string{"replica1", "replica2", "replica3"}
	p.SetReadyReplicas(replicas)

	// Test SelectReplica
	for i := 0; i < len(replicas); i++ {
		replica := p.SelectReplica(nil)
		if *replica != replicas[i] {
			t.Errorf("Expected replica %s, got %s", replicas[i], *replica)
		}
	}

	// After 2 seconds, suppose the request sent to replica2 is completed
	time.Sleep(2 * time.Second)
	p.UpdateNumberOfRequests("replica2")

	// Test UpdateNumberOfRequests behavior
	leastRequestsReplica := p.SelectReplica(nil)
	if *leastRequestsReplica != replicas[1] {
		t.Errorf("Expected least requests replica to be %s, got %s", replicas[0], *leastRequestsReplica)
	}

	// Test behavior when no replicas are available
	p.SetReadyReplicas([]string{})
	if p.SelectReplica(nil) != nil {
		t.Error("Expected nil when no replicas are available")
	}
}
