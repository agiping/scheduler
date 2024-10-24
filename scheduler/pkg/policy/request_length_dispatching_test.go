package policy

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"scheduler/scheduler/pkg/logger"
	"scheduler/scheduler/pkg/types"
)

func TestSetReadyReplicas(t *testing.T) {
	logger.Init("info")
	policy := NewRequestLengthDispatchingPolicy(20000)

	// Initial replicas
	initialReplicas := map[string][]string{
		"service1": {"service1-192.168.1.1:8080", "service1-192.168.1.2:8080"},
	}

	// Set initial replicas
	policy.SetReadyReplicas(initialReplicas)

	// Check if initial replicas are set correctly
	if len(policy.GetReadyReplicas()) != 2 {
		t.Errorf("Expected 2 replicas, got %d", len(policy.GetReadyReplicas()))
	}

	for _, pod := range policy.GetReadyReplicas() {
		fmt.Println(pod.IP)
	}

	fmt.Println("--------------------------------")
	// Update replicas
	updatedReplicas := map[string][]string{
		"service1": {"service1-192.168.1.1:8080", "service1-192.168.1.3:8080"}, // existing
	}

	// Set updated replicas
	policy.SetReadyReplicas(updatedReplicas)

	// Check if replicas are updated correctly
	readyReplicas := policy.GetReadyReplicas()
	for _, pod := range readyReplicas {
		fmt.Println(pod.IP)
	}

	if len(readyReplicas) != 2 {
		t.Errorf("Expected 2 replicas, got %d", len(readyReplicas))
	}

	// Check if the correct replicas are present
	expectedIPs := map[string]bool{
		"192.168.1.1:8080": true,
		"192.168.1.2:8080": false,
		"192.168.1.3:8080": true,
	}

	for _, pod := range readyReplicas {
		fmt.Println(pod.OwnerService)
		if !expectedIPs[pod.IP] {
			t.Errorf("Unexpected replica IP: %s", pod.IP)
		}
	}
}

// Helper function to create a pod
func createPod(ip, serviceName string, numRequests int) *types.Pod {
	return &types.Pod{
		IP:               ip,
		OwnerService:     serviceName,
		NumberOfRequests: numRequests,
	}
}

func TestSelectReplica_NoReplicas(t *testing.T) {
	logger.Init("info")
	policy := NewRequestLengthDispatchingPolicy(100)
	request := &types.InferRequest{RequestID: "req-1", PromptLength: 50}

	// No replicas available
	selectedReplica := policy.SelectReplica(request)
	assert.Equal(t, "", selectedReplica)
}

func TestSelectReplica_SmallModelShortRequest(t *testing.T) {
	logger.Init("info")
	policy := NewRequestLengthDispatchingPolicy(100)
	policy.ReadyReplicas = []*types.Pod{
		createPod("10.0.0.1:8080", "2-cards-service", 1),
	}
	request := &types.InferRequest{RequestID: "req-2", PromptLength: 50}

	// short request, small model, no penalty
	selectedReplica := policy.SelectReplica(request)
	assert.Equal(t, "10.0.0.1:8080", selectedReplica)
}

func TestSelectReplica_SmallModelLongRequest(t *testing.T) {
	logger.Init("info")
	policy := NewRequestLengthDispatchingPolicy(100)
	policy.ReadyReplicas = []*types.Pod{
		createPod("10.0.0.2:8080", "2-cards-service", 1),
	}
	request := &types.InferRequest{RequestID: "req-3", PromptLength: 150}

	// long request, small model, penalty applied
	selectedReplica := policy.SelectReplica(request)
	assert.Equal(t, "10.0.0.2:8080", selectedReplica)
	assert.Equal(t, 2, policy.ReadyReplicas[0].NumberOfRequests)
}

func TestSelectReplica_LargeModelShortRequest(t *testing.T) {
	logger.Init("info")
	policy := NewRequestLengthDispatchingPolicy(100)
	policy.ReadyReplicas = []*types.Pod{
		createPod("10.0.0.3:8080", "4-cards-service", 2),
	}
	request := &types.InferRequest{RequestID: "req-4", PromptLength: 50}

	// Short request, large model, penalty applied
	selectedReplica := policy.SelectReplica(request)
	assert.Equal(t, "10.0.0.3:8080", selectedReplica)
	assert.Equal(t, 3, policy.ReadyReplicas[0].NumberOfRequests)
}

func TestSelectReplica_LargeModelLongRequest(t *testing.T) {
	logger.Init("info")
	policy := NewRequestLengthDispatchingPolicy(100)
	policy.ReadyReplicas = []*types.Pod{
		createPod("10.0.0.4:8080", "4-cards-service", 3),
	}
	request := &types.InferRequest{RequestID: "req-5", PromptLength: 150}

	// long request, large model, no penalty
	selectedReplica := policy.SelectReplica(request)
	assert.Equal(t, "10.0.0.4:8080", selectedReplica)
	assert.Equal(t, 4, policy.ReadyReplicas[0].NumberOfRequests)
}

func TestSelectReplica_MultipleReplicas(t *testing.T) {
	logger.Init("info")
	request := &types.InferRequest{RequestID: "req-6", PromptLength: 50}

	policy := NewRequestLengthDispatchingPolicy(100)
	// should select the 2-cards replica
	policy.ReadyReplicas = []*types.Pod{
		createPod("10.0.0.5:8080", "2-cards-service", 5),
		createPod("10.0.0.6:8080", "4-cards-service", 1),
	}
	selectedReplica := policy.SelectReplica(request)
	assert.Equal(t, "10.0.0.5:8080", selectedReplica)

	// should select the 4-cards replica
	policy.ReadyReplicas = []*types.Pod{
		createPod("10.0.0.7:8080", "2-cards-service", 25),
		createPod("10.0.0.8:8080", "4-cards-service", 1),
	}
	selectedReplica = policy.SelectReplica(request)
	assert.Equal(t, "10.0.0.8:8080", selectedReplica)

	// should select the 2-cards replica
	request = &types.InferRequest{RequestID: "req-7", PromptLength: 150}
	policy.ReadyReplicas = []*types.Pod{
		createPod("10.0.0.9:8080", "2-cards-service", 2),
		createPod("10.0.0.10:8080", "4-cards-service", 28),
	}
	selectedReplica = policy.SelectReplica(request)
	assert.Equal(t, "10.0.0.9:8080", selectedReplica)

	// should select the 4-cards replica
	request = &types.InferRequest{RequestID: "req-8", PromptLength: 50}
	policy.ReadyReplicas = []*types.Pod{
		createPod("10.0.0.11:8080", "2-cards-service", 25),
		createPod("10.0.0.12:8080", "2-cards-service", 24),
		createPod("10.0.0.13:8080", "4-cards-service", 2),
		createPod("10.0.0.14:8080", "4-cards-service", 5),
	}
	selectedReplica = policy.SelectReplica(request)
	assert.Equal(t, "10.0.0.13:8080", selectedReplica)
}

func TestUpdateAfterResponse(t *testing.T) {
	logger.Init("info")
	policy := NewRequestLengthDispatchingPolicy(100)
	policy.ReadyReplicas = []*types.Pod{
		createPod("10.0.0.15:8080", "2-cards-service", 2),
		createPod("10.0.0.16:8080", "2-cards-service", 3),
		createPod("10.0.0.17:8080", "4-cards-service", 2),
	}
	request := &types.InferRequest{RequestID: "req-9", PromptLength: 50}
	selectedReplica := policy.SelectReplica(request)
	assert.Equal(t, "10.0.0.15:8080", selectedReplica)

	policy.UpdateAfterResponse(selectedReplica)

	assert.Equal(t, 3, len(policy.ReadyReplicas))
	assert.Equal(t, "10.0.0.15:8080", policy.ReadyReplicas[0].IP)
	assert.Equal(t, 2, policy.ReadyReplicas[0].NumberOfRequests)

	expectedNumberOfRequests := []int{2, 3, 2}
	for i, pod := range policy.ReadyReplicas {
		assert.Equal(t, expectedNumberOfRequests[i], pod.NumberOfRequests)
	}
}

func TestSelectReplicaForRetry(t *testing.T) {
	logger.Init("info")
	policy := NewRequestLengthDispatchingPolicy(100)
	policy.ReadyReplicas = []*types.Pod{
		createPod("10.0.0.18:8080", "2-cards-service", 2),
		createPod("10.0.0.19:8080", "2-cards-service", 3),
		createPod("10.0.0.20:8080", "4-cards-service", 2),
	}
	request := &types.InferRequest{RequestID: "req-10", PromptLength: 50}
	selectedReplica := policy.SelectReplica(request)
	assert.Equal(t, "10.0.0.18:8080", selectedReplica)

	selectedReplicaForRetry := policy.SelectReplicaForRetry("req-10", 50, "10.0.0.18:8080")
	assert.Equal(t, "10.0.0.19:8080", selectedReplicaForRetry)

	policy.ReadyReplicas = []*types.Pod{
		createPod("10.0.0.21:8080", "2-cards-service", 24),
		createPod("10.0.0.22:8080", "2-cards-service", 25),
		createPod("10.0.0.23:8080", "4-cards-service", 2),
	}

	selectedReplicaForRetry = policy.SelectReplicaForRetry("req-10", 50, "10.0.0.21:8080")
	assert.Equal(t, "10.0.0.23:8080", selectedReplicaForRetry)
}
