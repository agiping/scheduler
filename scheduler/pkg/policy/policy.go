package policy

import (
	"scheduler/scheduler/pkg/types"

	"sync"
)

// LoadBalancingPolicy is an interface for load balancing policies.
type LoadBalancingPolicy interface {
	GetLock() sync.Locker
	GetReadyReplicas() []*types.Pod
	GetStringReadyReplicas() []string
	SetReadyReplicas(map[string][]string)
	SelectReplica(*types.InferRequest) string
	SelectReplicaForRetry(string, int, string) string
	UpdateAfterResponse(string)
	UpdateTgiQueueSize(*sync.Map)
	GetPolicyName() string
	PrintNumberOfRequests()
	GetNumberOfRequests() map[string]int
}
