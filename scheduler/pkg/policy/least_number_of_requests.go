package policy

import (
	"log"
	"net/http"
	"sync"
)

// LeastNumberOfRequestsPolicy implements the LoadBalancingPolicy using the least number of requests strategy.
type LeastNumberOfRequestsPolicy struct {
	ReadyReplicas    []string
	connectionsCount map[string]int
	PoLock           sync.RWMutex // use a read-write lock to allow multiple readers.
}

// NewLeastNumberOfRequestsPolicy creates a new instance of LeastNumberOfRequestsPolicy.
func NewLeastNumberOfRequestsPolicy() *LeastNumberOfRequestsPolicy {
	return &LeastNumberOfRequestsPolicy{
		connectionsCount: make(map[string]int),
	}
}

// SetReadyReplicas sets the list of available replicas.
func (p *LeastNumberOfRequestsPolicy) SetReadyReplicas(replicas []string) {
	p.PoLock.Lock()
	defer p.PoLock.Unlock()

	newConnectionsCount := make(map[string]int)
	for _, replica := range replicas {
		newConnectionsCount[replica] = p.connectionsCount[replica] // retain existing count or default to 0
	}

	p.ReadyReplicas = replicas
	p.connectionsCount = newConnectionsCount
}

// SelectReplica selects the replica with the least number of connections.
func (p *LeastNumberOfRequestsPolicy) SelectReplica(request *http.Request) string {
	p.PoLock.RLock()
	defer p.PoLock.RUnlock()

	if len(p.ReadyReplicas) == 0 {
		log.Printf("No replicas available for request %v\n", request)
		return ""
	}

	var selectedReplica string
	minConnections := int(^uint(0) >> 1) // max int value

	for _, replica := range p.ReadyReplicas {
		if p.connectionsCount[replica] < minConnections {
			selectedReplica = replica
			minConnections = p.connectionsCount[replica]
		}
	}

	p.connectionsCount[selectedReplica]++
	log.Printf("Selected replica %s for request %v\n", selectedReplica, request)
	return selectedReplica
}

// UpdateNumberOfRequests records a connection to a replica.
func (p *LeastNumberOfRequestsPolicy) UpdateAfterResponse(replica string) {
	p.PoLock.Lock()
	defer p.PoLock.Unlock()

	if _, ok := p.connectionsCount[replica]; ok {
		p.connectionsCount[replica] = max(0, p.connectionsCount[replica]-1)
	}
}
