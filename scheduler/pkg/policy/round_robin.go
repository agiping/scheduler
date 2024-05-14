package policy

import (
	"log"
	"math/rand"
	"sync"
	"time"

	"scheduler/scheduler/pkg/types"
)

// RoundRobinPolicy implements the LoadBalancingPolicy using a round-robin strategy.
type RoundRobinPolicy struct {
	ReadyReplicas []string
	index         int
	PoLock        sync.RWMutex
}

// NewRoundRobinPolicy creates a new instance of RoundRobinPolicy.
func NewRoundRobinPolicy() *RoundRobinPolicy {
	return &RoundRobinPolicy{}
}

// SetReadyReplicas sets the list of available replicas.
func (p *RoundRobinPolicy) SetReadyReplicas(replicas []string) {
	p.PoLock.Lock()
	defer p.PoLock.Unlock()

	// Shuffle replicas to prevent loading the first one too heavily
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(replicas), func(i, j int) {
		replicas[i], replicas[j] = replicas[j], replicas[i]
	})

	p.ReadyReplicas = replicas
	p.index = 0
}

// SelectReplica selects the next replica in a round-robin fashion.
func (p *RoundRobinPolicy) SelectReplica(request *types.InferRequest) string {
	p.PoLock.RLock()
	defer p.PoLock.RUnlock()

	if len(p.ReadyReplicas) == 0 {
		return ""
	}

	replica := p.ReadyReplicas[p.index]
	p.index = (p.index + 1) % len(p.ReadyReplicas)

	log.Printf("Selected replica %s for request %v", replica, request)
	return replica
}

func (p *RoundRobinPolicy) SelectReplicaForRetry(request *types.InferRequest, replica string) string {
	return "Not implemented yet"
}

func (p *RoundRobinPolicy) GetLock() sync.Locker {
	return &p.PoLock
}

func (p *RoundRobinPolicy) GetReadyReplicas() []*types.Pod {
	p.PoLock.RLock()
	defer p.PoLock.RUnlock()

	replicas := make([]*types.Pod, len(p.ReadyReplicas))
	for i, replica := range p.ReadyReplicas {
		replicas[i].IP = replica
	}
	return replicas
}

func (p *RoundRobinPolicy) UpdateAfterResponse(podIP string) {
	log.Println("request finished on pod: ", podIP)
}

func (p *RoundRobinPolicy) UpdateTgiQueueSize(*sync.Map) {
	log.Println("Not implemented yet")
}

func (p *RoundRobinPolicy) GetPolicyName() string {
	return "RoundRobin"
}
