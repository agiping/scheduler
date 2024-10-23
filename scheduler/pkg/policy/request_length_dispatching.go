package policy

import (
	"scheduler/scheduler/pkg/logger"
	"scheduler/scheduler/pkg/types"
	"strings"
	"sync"
)

// We have some established norms for the request length dispatching policy.
const SmallTgiModel = "2-cards"
const LargeTgiModel = "4-cards"

const PenaltyForShortRequestToLargeModel = 10 // P
const PenaltyForLongRequestToSmallModel = 20  // M

type RequestLengthDispatchingPolicy struct {
	// TODO(Ping Zhang): We may consider using a more sophisticated policy
	// to dispatch requests: length-dispatching with cache-aware.
	// CacheAwarePolicy *CacheAwarePolicy
	PolicyLock        sync.RWMutex
	ReadyReplicas     []*types.Pod
	StringReplicas    []string
	DispatchThreshold int
}

func NewRequestLengthDispatchingPolicy(dispatchThreshold int) *RequestLengthDispatchingPolicy {
	return &RequestLengthDispatchingPolicy{
		ReadyReplicas:     make([]*types.Pod, 0),
		StringReplicas:    make([]string, 0),
		DispatchThreshold: dispatchThreshold,
	}
}

func (p *RequestLengthDispatchingPolicy) GetReadyReplicas() []*types.Pod {
	return p.ReadyReplicas
}

func (p *RequestLengthDispatchingPolicy) GetStringReadyReplicas() []string {
	return p.StringReplicas
}

func (p *RequestLengthDispatchingPolicy) SetReadyReplicas(replicas []string) {
	// replica format: serviceName-podip:port
	p.PolicyLock.Lock()
	defer p.PolicyLock.Unlock()

	newReplicaMap := make(map[string]*types.Pod)
	for _, servicePod := range replicas {
		replicaSplited := strings.Split(servicePod, "-")
		serviceName := replicaSplited[0]
		ipport := replicaSplited[1]
		newReplicaMap[ipport] = &types.Pod{IP: ipport, OwnerService: serviceName}
	}

	updatedReplicas := []*types.Pod{}
	for _, pod := range p.ReadyReplicas {
		if _, exists := newReplicaMap[pod.IP]; exists {
			updatedReplicas = append(updatedReplicas, pod)
			delete(newReplicaMap, pod.IP)
		}
	}

	// Add new replicas scaled up by autoscaler
	for _, pod := range newReplicaMap {
		updatedReplicas = append(updatedReplicas, pod)
	}

	// Replace the old slice with the updated one
	p.ReadyReplicas = updatedReplicas
}

func (p *RequestLengthDispatchingPolicy) SelectReplica(request *types.InferRequest) string {
	p.PolicyLock.RLock()
	defer p.PolicyLock.RUnlock()

	if len(p.ReadyReplicas) == 0 {
		logger.Log.Warnf("No replicas available for request %v", request)
		return ""
	}

	var selectedReplica *types.Pod
	minCost := int(^uint(0) >> 1) // max int value
	inputLength := request.PromptLength

	for _, replica := range p.ReadyReplicas {
		cost := p.computeCost(inputLength, replica)
		if cost < minCost {
			selectedReplica = replica
			minCost = cost
		}
	}
	logger.Log.Infof("current min cost: %d", minCost)
	selectedReplica.NumberOfRequests++
	logger.Log.Infof("Selected replica %s for request %s", selectedReplica.IP, request.RequestID)
	// p.PrintNumberOfRequests()
	return selectedReplica.IP
}

func (p *RequestLengthDispatchingPolicy) SelectReplicaForRetry(requestID, currentReplica string) string {
	return "Not implemented yet"
}

func (p *RequestLengthDispatchingPolicy) UpdateAfterResponse(replica string) {
	p.PolicyLock.Lock()
	defer p.PolicyLock.Unlock()

	for _, pod := range p.ReadyReplicas {
		if pod.IP == replica && pod.NumberOfRequests > 0 {
			pod.NumberOfRequests--
			return
		}
	}
}

func (p *RequestLengthDispatchingPolicy) UpdateTgiQueueSize(queueSizes *sync.Map) {
	logger.Log.Infof("Not implemented yet")
}

func (p *RequestLengthDispatchingPolicy) GetPolicyName() string {
	return "request-length-dispatching"
}

func (p *RequestLengthDispatchingPolicy) PrintNumberOfRequests() {
	logger.Log.Infof("Not implemented yet")
}

func (p *RequestLengthDispatchingPolicy) GetNumberOfRequests() map[string]int {
	logger.Log.Infof("Not implemented yet")
	return nil
}

func (p *RequestLengthDispatchingPolicy) GetLock() sync.Locker {
	return &p.PolicyLock
}

/*
cost matrix:

	2-GPU-instance           4-GPU-instance

< threshold:  num_requests             num_requests + P
>= threshold: num_requests + M         num_requests
*/
func (p *RequestLengthDispatchingPolicy) computeCost(inputLength int, pod *types.Pod) int {
	serviceName := pod.OwnerService
	switch {
	case strings.Contains(serviceName, SmallTgiModel) && inputLength < p.DispatchThreshold:
		return pod.NumberOfRequests
	case strings.Contains(serviceName, SmallTgiModel) && inputLength >= p.DispatchThreshold:
		return pod.NumberOfRequests + PenaltyForLongRequestToSmallModel
	case strings.Contains(serviceName, LargeTgiModel) && inputLength < p.DispatchThreshold:
		return pod.NumberOfRequests + PenaltyForShortRequestToLargeModel
	case strings.Contains(serviceName, LargeTgiModel) && inputLength >= p.DispatchThreshold:
		return pod.NumberOfRequests
	}
	logger.Log.Errorf("Unsupported service naming convention for request length dispatching policy: %s", serviceName)
	return 0
}
