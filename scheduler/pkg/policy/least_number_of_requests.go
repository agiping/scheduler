package policy

import (
	"fmt"
	"log"
	"sync"

	"scheduler/scheduler/pkg/types"
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
		connectionsCount: new(sync.Map), // 初始化 sync.Map
	}
}

// SetReadyReplicas sets the list of available replicas.
func (p *LeastNumberOfRequestsPolicy) SetReadyReplicas(replicas []string) {
	p.PoLock.Lock()
	defer p.PoLock.Unlock()

	// 创建一个新的 sync.Map 以保存更新后的连接计数
	newConnectionsCount := &sync.Map{}

	// 为新的副本列表保留或初始化连接计数
	for _, replica := range replicas {
		if val, ok := p.connectionsCount.Load(replica); ok {
			newConnectionsCount.Store(replica, val) // 保留现有的计数
		} else {
			newConnectionsCount.Store(replica, 0) // 新副本初始化为 0
		}
	}

	p.ReadyReplicas = replicas
	p.connectionsCount = newConnectionsCount // 更新连接计数的 sync.Map
}

// SelectReplica selects the replica with the least number of connections.
func (p *LeastNumberOfRequestsPolicy) SelectReplica(request *types.InferRequest) string {
	if len(p.ReadyReplicas) == 0 {
		log.Printf("No replicas available for request %v\n", request)
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

	log.Printf("Selected replica %s for request %s\n", selectedReplica, request.RequestID)
	p.printNumberOfRequests()
	return selectedReplica
}

// UpdateNumberOfRequests records a connection to a replica.
func (p *LeastNumberOfRequestsPolicy) UpdateAfterResponse(replica string) {
	p.PoLock.Lock()
	defer p.PoLock.Unlock()

	// 加载指定副本的当前连接数
	if currentValue, ok := p.connectionsCount.Load(replica); ok {
		// 确保连接数不会变成负数
		newValue := max(0, currentValue.(int)-1)

		// 更新连接数
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
	fmt.Println("Not implemented yet")
}

func (p *LeastNumberOfRequestsPolicy) printNumberOfRequests() {
	p.connectionsCount.Range(func(key, value interface{}) bool {
		fmt.Printf("Pod: %v, Number Of Requests: %v\n", key, value)
		return true
	})
}
