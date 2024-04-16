package policy

import (
	"log"
	"math/rand"
	"sync"
	"time"
)

// RoundRobinPolicy implements the LoadBalancingPolicy using a round-robin strategy.
type RoundRobinPolicy struct {
	readyReplicas []string
	index         int
	lock          sync.Mutex
}

// NewRoundRobinPolicy creates a new instance of RoundRobinPolicy.
func NewRoundRobinPolicy() *RoundRobinPolicy {
	return &RoundRobinPolicy{}
}

// SetReadyReplicas sets the list of available replicas.
func (p *RoundRobinPolicy) SetReadyReplicas(replicas []string) {
	p.lock.Lock()
	defer p.lock.Unlock()

	// Shuffle replicas to prevent loading the first one too heavily
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(replicas), func(i, j int) {
		replicas[i], replicas[j] = replicas[j], replicas[i]
	})

	p.readyReplicas = replicas
	p.index = 0
}

// SelectReplica selects the next replica in a round-robin fashion.
func (p *RoundRobinPolicy) SelectReplica(request *Request) *string {
	p.lock.Lock()
	defer p.lock.Unlock()

	if len(p.readyReplicas) == 0 {
		return nil
	}

	replica := p.readyReplicas[p.index]
	p.index = (p.index + 1) % len(p.readyReplicas)

	log.Printf("Selected replica %s for request %v\n", replica, request)
	return &replica
}
