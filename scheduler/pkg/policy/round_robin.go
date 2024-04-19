package policy

import (
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
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
func (p *RoundRobinPolicy) SelectReplica(request *http.Request) *string {
	p.PoLock.RLock()
	defer p.PoLock.RUnlock()

	if len(p.ReadyReplicas) == 0 {
		return nil
	}

	replica := p.ReadyReplicas[p.index]
	p.index = (p.index + 1) % len(p.ReadyReplicas)

	log.Printf("Selected replica %s for request %v\n", replica, request)
	return &replica
}
