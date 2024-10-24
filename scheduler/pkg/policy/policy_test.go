package policy

import (
	"scheduler/scheduler/pkg/logger"
	"scheduler/scheduler/pkg/types"
	"testing"
	"time"
)

func TestRoundRobinPolicy(t *testing.T) {
	p := NewRoundRobinPolicy()

	// Test SetReadyReplicas
	replicas := map[string][]string{"service1": {"replica1", "replica2", "replica3"}}
	p.SetReadyReplicas(replicas)

	// Test SelectReplica
	for i := 0; i < len(replicas["service1"]); i++ {
		replica := p.SelectReplica(nil)
		if replica != replicas["service1"][i] {
			t.Errorf("Expected replica %s, got %s", replicas["service1"][i], replica)
		}
	}

	// Test round-robin behavior
	firstReplica := p.SelectReplica(nil)
	if firstReplica != replicas["service1"][0] {
		t.Errorf("Expected first replica in the next round to be %s, got %s", replicas["service1"][0], firstReplica)
	}

	// Test behavior when no replicas are available
	p.SetReadyReplicas(map[string][]string{})
	if p.SelectReplica(nil) != "" {
		t.Error("Expected nil when no replicas are available")
	}
}

func TestLeastNumberOfRequestsPolicy(t *testing.T) {
	logger.Init("info")
	p := NewLeastNumberOfRequestsPolicy()

	// Test SetReadyReplicas
	replicas := map[string][]string{"service1": {"replica1", "replica2", "replica3"}}
	p.SetReadyReplicas(replicas)

	request := &types.InferRequest{
		RequestID: "request-1",
	}
	// Test SelectReplica
	for i := 0; i < len(replicas["service1"]); i++ {
		replica := p.SelectReplica(request)
		if replica != replicas["service1"][i] {
			t.Errorf("Expected replica %s, got %s", replicas["service1"][i], replica)
		}
	}

	// After 2 seconds, suppose the request sent to replica2 is completed
	time.Sleep(2 * time.Second)
	p.UpdateAfterResponse("replica2")

	// Test UpdateNumberOfRequests behavior
	leastRequestsReplica := p.SelectReplica(request)
	if leastRequestsReplica != replicas["service1"][1] {
		t.Errorf("Expected least requests replica to be %s, got %s", replicas["service1"][1], leastRequestsReplica)
	}

	// Test behavior when no replicas are available
	p.SetReadyReplicas(map[string][]string{})
	if p.SelectReplica(request) != "" {
		t.Error("Expected nil when no replicas are available")
	}
}
