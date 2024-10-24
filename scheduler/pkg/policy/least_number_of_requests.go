package policy

import (
	"sync"

	"scheduler/scheduler/pkg/logger"
	"scheduler/scheduler/pkg/types"
	"scheduler/scheduler/pkg/utils"
)

// LeastNumberOfRequestsPolicy implements the LoadBalancingPolicy using the least number of requests strategy.
type LeastNumberOfRequestsPolicy struct {
	ReadyReplicas    []string
	connectionsCount *sync.Map
	PoLock           sync.RWMutex // use a read-write lock to allow multiple readers.
}

// NewLeastNumberOfRequestsPolicy creates a new instance of LeastNumberOfRequestsPolicy.
func NewLeastNumberOfRequestsPolicy() *LeastNumberOfRequestsPolicy {
	return &LeastNumberOfRequestsPolicy{
		ReadyReplicas:    []string{},
		connectionsCount: new(sync.Map), // initialize sync.Map
	}
}

// SetReadyReplicas sets the list of available replicas.
func (p *LeastNumberOfRequestsPolicy) SetReadyReplicas(replicas map[string][]string) {
	// replica format: podip:port
	p.PoLock.Lock()
	defer p.PoLock.Unlock()

	newConnectionsCount := &sync.Map{}

	_, serviceReplicas := utils.GetFirstKeyVaule(replicas)

	for _, replica := range serviceReplicas {
		if val, ok := p.connectionsCount.Load(replica); ok {
			newConnectionsCount.Store(replica, val) // Keep the existing connection count
		} else {
			newConnectionsCount.Store(replica, 0) // Initialize the connection count to 0
		}
	}

	p.ReadyReplicas = serviceReplicas
	p.connectionsCount = newConnectionsCount
}

// SelectReplica selects the replica with the least number of connections.
func (p *LeastNumberOfRequestsPolicy) SelectReplica(request *types.InferRequest) string {
	if len(p.ReadyReplicas) == 0 {
		logger.Log.Warnf("No replicas available for request %v", request)
		return ""
	}

	var selectedReplica string
	minConnections := int(^uint(0) >> 1) // max int value

	for _, replica := range p.ReadyReplicas {
		connCount, _ := p.connectionsCount.Load(replica)
		if connCount.(int) < minConnections {
			selectedReplica = replica
			minConnections = connCount.(int)
		}
	}

	currentCount, _ := p.connectionsCount.LoadOrStore(selectedReplica, 0)
	p.connectionsCount.Store(selectedReplica, currentCount.(int)+1)

	logger.Log.Infof("Selected replica %s for request %s", selectedReplica, request.RequestID)
	p.PrintNumberOfRequests()
	return selectedReplica
}

func (p *LeastNumberOfRequestsPolicy) SelectReplicaForRetry(requestID string, promptLength int, currentReplica string) string {
	if len(p.ReadyReplicas) == 0 {
		logger.Log.Warnf("No replicas available for retry requestID: %s", requestID)
		return ""
	}

	var selectedReplica string
	minConnections := int(^uint(0) >> 1) // max int value

	for _, replica := range p.ReadyReplicas {
		if replica == currentReplica {
			continue
		}
		connCount, _ := p.connectionsCount.Load(replica)
		if connCount.(int) < minConnections {
			selectedReplica = replica
			minConnections = connCount.(int)
		}
	}

	// If no other replica is found for this retry, return empty
	if selectedReplica == "" {
		logger.Log.Warnf("No other replicas available for retry requestID: %s", requestID)
		return ""
	}

	// Update the connection count for the selected replica
	currentCount, _ := p.connectionsCount.LoadOrStore(selectedReplica, 0)
	p.connectionsCount.Store(selectedReplica, currentCount.(int)+1)

	logger.Log.Infof("Selected replica %s for retry request %s", selectedReplica, requestID)
	p.PrintNumberOfRequests()
	return selectedReplica
}

// UpdateNumberOfRequests records a connection to a replica.
func (p *LeastNumberOfRequestsPolicy) UpdateAfterResponse(replica string) {
	p.PoLock.Lock()
	defer p.PoLock.Unlock()

	if currentValue, ok := p.connectionsCount.Load(replica); ok {
		// make sure the value is not negative
		newValue := max(0, currentValue.(int)-1)

		p.connectionsCount.Store(replica, newValue)
	}
}

func (p *LeastNumberOfRequestsPolicy) GetLock() sync.Locker {
	return &p.PoLock
}

func (p *LeastNumberOfRequestsPolicy) GetReadyReplicas() []*types.Pod {
	p.PoLock.RLock()
	defer p.PoLock.RUnlock()

	replicas := make([]*types.Pod, len(p.ReadyReplicas))
	for i, replica := range p.ReadyReplicas {
		replicas[i].IP = replica
	}
	return replicas
}

func (p *LeastNumberOfRequestsPolicy) UpdateTgiQueueSize(*sync.Map) {
	logger.Log.Info("Not implemented yet")
}

// For debug purposes.
func (p *LeastNumberOfRequestsPolicy) PrintNumberOfRequests() {
	nor := p.GetNumberOfRequests()
	logger.Log.Infof("Number of Requests per Replica: %v", nor)
}

func (p *LeastNumberOfRequestsPolicy) GetPolicyName() string {
	return "LeastNumberOfRequests"
}

func (p *LeastNumberOfRequestsPolicy) GetStringReadyReplicas() []string {
	return p.ReadyReplicas
}

func (p *LeastNumberOfRequestsPolicy) GetNumberOfRequests() map[string]int {
	p.PoLock.RLock()
	defer p.PoLock.RUnlock()

	requestsPerReplica := make(map[string]int)

	p.connectionsCount.Range(func(key, value interface{}) bool {
		replica, ok := key.(string)
		if !ok {
			return true
		}
		count, ok := value.(int)
		if !ok {
			return true
		}
		requestsPerReplica[replica] = count
		return true
	})

	return requestsPerReplica
}
