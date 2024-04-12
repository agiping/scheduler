package scheduler

import (
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// Request is a placeholder for the HTTP request.
// We may need to modify this to suit the HTTP framework you are using.
type Request struct {
	Method      string
	URL         string
	Headers     map[string]string
	QueryParams map[string]string
}

// LoadBalancingPolicy is an interface for load balancing policies.
type LoadBalancingPolicy interface {
	SetReadyReplicas(replicas []string)
	SelectReplica(request *http.Request) *string
	ReleaseConnection(replica string)
}

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

// LeastNumberOfRequestsPolicy implements the LoadBalancingPolicy using the least number of requests strategy.
type LeastNumberOfRequestsPolicy struct {
	readyReplicas    []string
	connectionsCount map[string]int
	lock             sync.Mutex
}

// NewLeastNumberOfRequestsPolicy creates a new instance of LeastNumberOfRequestsPolicy.
func NewLeastNumberOfRequestsPolicy() *LeastNumberOfRequestsPolicy {
	return &LeastNumberOfRequestsPolicy{
		connectionsCount: make(map[string]int),
	}
}

// SetReadyReplicas sets the list of available replicas.
func (p *LeastNumberOfRequestsPolicy) SetReadyReplicas(replicas []string) {
	p.lock.Lock()
	defer p.lock.Unlock()

	newConnectionsCount := make(map[string]int)
	for _, replica := range replicas {
		newConnectionsCount[replica] = p.connectionsCount[replica] // retain existing count or default to 0
	}

	p.readyReplicas = replicas
	p.connectionsCount = newConnectionsCount
}

// SelectReplica selects the replica with the least number of connections.
func (p *LeastNumberOfRequestsPolicy) SelectReplica(request *Request) *string {
	p.lock.Lock()
	defer p.lock.Unlock()

	if len(p.readyReplicas) == 0 {
		return nil
	}

	var selectedReplica string
	minConnections := int(^uint(0) >> 1) // max int value

	for _, replica := range p.readyReplicas {
		if p.connectionsCount[replica] < minConnections {
			selectedReplica = replica
			minConnections = p.connectionsCount[replica]
		}
	}

	p.connectionsCount[selectedReplica]++
	log.Printf("Selected replica %s for request %v\n", selectedReplica, request)
	return &selectedReplica
}

// func main() {
// 	// Example usage
// 	policy := NewLeastNumberOfRequestsPolicy()
// 	policy.SetReadyReplicas([]string{"http://server1:8080", "http://server2:8080", "http://server3:8080"})

// 	request := &Request{
// 		Method:      "GET",
// 		URL:         "http://example.com",
// 		Headers:     map[string]string{"Content-Type": "application/json"},
// 		QueryParams: map[string]string{"q": "query"},
// 	}
// 	selectedReplica := policy.SelectReplica(request)
// 	if selectedReplica != nil {
// 		log.Println("Request will be sent to:", *selectedReplica)
// 	}
// }
