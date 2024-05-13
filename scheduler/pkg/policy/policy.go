package policy

import (
	"scheduler/scheduler/pkg/types"

	"sync"
)

// LoadBalancingPolicy is an interface for load balancing policies.
type LoadBalancingPolicy interface {
	GetLock() sync.Locker
	GetReadyReplicas() []*types.Pod
	SetReadyReplicas([]string)
	SelectReplica(*types.InferRequest) string
	SelectReplicaForRetry(*types.InferRequest, string) string
	UpdateAfterResponse(string)
	UpdateTgiQueueSize(*sync.Map)
}
