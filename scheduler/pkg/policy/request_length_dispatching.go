package policy

import (
	"regexp"
	"scheduler/scheduler/pkg/logger"
	"scheduler/scheduler/pkg/types"
	"scheduler/scheduler/pkg/utils"
	"strings"
	"sync"
)

// max int value
const MaxInt = int(^uint(0) >> 1)

// We have some established norms for the request length dispatching policy.
const SmallTgiModel = "2-cards"
const LargeTgiModel = "4-cards"

// instance format: serviceName-podip:port; e.g. chat-service-2-cards-10.0.0.1:80
var InstanceRegex = regexp.MustCompile(`^(.+)-(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}:\d+)$`)

const PenaltyForShortRequestToLargeModel = 10 // P
const PenaltyForLongRequestToSmallModel = 20  // M

type RequestLengthDispatchingPolicy struct {
	// TODO(Ping Zhang): We may consider using a more sophisticated policy
	// to dispatch requests: length-dispatching with cache-aware.
	// CacheAwarePolicy *CacheAwarePolicy
	PolicyLock              sync.RWMutex
	ReadyReplicas           []*types.Pod            // [pod1, pod2, pod3, pod4...]
	ReadyReplicasPerService map[string][]*types.Pod // {serviceName1: [pod1, pod2, ...], serviceName2: [pod3, pod4, ...]}
	StringReplicas          []string
	DispatchThreshold       int
}

func NewRequestLengthDispatchingPolicy(dispatchThreshold int) *RequestLengthDispatchingPolicy {
	return &RequestLengthDispatchingPolicy{
		ReadyReplicasPerService: make(map[string][]*types.Pod),
		ReadyReplicas:           make([]*types.Pod, 0),
		StringReplicas:          make([]string, 0),
		DispatchThreshold:       dispatchThreshold,
	}
}

func (p *RequestLengthDispatchingPolicy) GetReadyReplicas() []*types.Pod {
	return p.ReadyReplicas
}

func (p *RequestLengthDispatchingPolicy) GetStringReadyReplicas() []string {
	p.PolicyLock.RLock()
	defer p.PolicyLock.RUnlock()

	for _, replica := range p.ReadyReplicas {
		p.StringReplicas = append(p.StringReplicas, replica.IP)
	}
	return p.StringReplicas
}

func (p *RequestLengthDispatchingPolicy) SetReadyReplicas(replicas map[string][]string) {
	p.PolicyLock.Lock()
	defer p.PolicyLock.Unlock()

	serviceName, serviceReplicas := utils.GetFirstKeyVaule(replicas)

	/*
		We are handling multiple services here. If one of the services has no replicas,
		i.g., the service is deleted, or the deployment backend has no available replicas,
		we need to initialize the replica list for that service to empty.
	*/
	if len(serviceReplicas) == 0 {
		p.ReadyReplicasPerService[serviceName] = make([]*types.Pod, 0)
	}
	// newReplicaMap stores per-service replicas
	// {serviceName1: {ip1: pod1, ip2: pod2, ...}, serviceName2: {ip3: pod3, ip4: pod4, ...}}
	newReplicaMap := make(map[string]map[string]*types.Pod)

	for _, servicePod := range serviceReplicas {
		match := InstanceRegex.FindStringSubmatch(servicePod)
		if len(match) != 3 {
			logger.Log.Errorf("Invalid replica format: %s", servicePod)
			continue
		}
		serviceName := match[1]
		ipport := match[2]

		if _, exists := newReplicaMap[serviceName]; !exists {
			newReplicaMap[serviceName] = make(map[string]*types.Pod)
		}
		newReplicaMap[serviceName][ipport] = &types.Pod{IP: ipport, OwnerService: serviceName}
	}

	// Handle each service's replicas
	for serviceName, replicas := range newReplicaMap {
		replicasOfCurrentService := p.ReadyReplicasPerService[serviceName]
		updatedReplicasOfCurrentService := make([]*types.Pod, 0)

		for _, pod := range replicasOfCurrentService {
			if _, exists := replicas[pod.IP]; exists {
				updatedReplicasOfCurrentService = append(updatedReplicasOfCurrentService, pod)
				delete(replicas, pod.IP)
			}
		}

		// Add new replicas scaled up by autoscaler
		for _, pod := range replicas {
			updatedReplicasOfCurrentService = append(updatedReplicasOfCurrentService, pod)
		}

		// Replace the old slice with the updated one for the current service
		p.ReadyReplicasPerService[serviceName] = updatedReplicasOfCurrentService
	}

	// Update the global ready replicas
	globalReadyReplicas := make([]*types.Pod, 0)
	for _, replicas := range p.ReadyReplicasPerService {
		globalReadyReplicas = append(globalReadyReplicas, replicas...)
	}
	p.ReadyReplicas = globalReadyReplicas
}

func (p *RequestLengthDispatchingPolicy) SelectReplica(request *types.InferRequest) string {
	p.PolicyLock.Lock()
	defer p.PolicyLock.Unlock()

	if len(p.ReadyReplicas) == 0 {
		logger.Log.Warnf("No replicas available for request %v", request)
		return ""
	}

	selectedReplica := p.selectBestReplica("", request.PromptLength)
	selectedReplica.NumberOfRequests++
	logger.Log.Infof("Selected replica %s for request %s", selectedReplica.IP, request.RequestID)
	return selectedReplica.IP
}

func (p *RequestLengthDispatchingPolicy) SelectReplicaForRetry(requestID string, promptLength int, currentReplica string) string {
	p.PolicyLock.Lock()
	defer p.PolicyLock.Unlock()

	if len(p.ReadyReplicas) == 0 {
		logger.Log.Warnf("No replicas available for retry request %s", requestID)
		return ""
	}

	selectedReplica := p.selectBestReplica(currentReplica, promptLength)

	if selectedReplica == nil {
		logger.Log.Warnf("No replicas available for retry request %s", requestID)
		return ""
	}

	selectedReplica.NumberOfRequests++
	logger.Log.Infof("Selected replica %s for retry request %s", selectedReplica.IP, requestID)
	return selectedReplica.IP
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
	p.PolicyLock.RLock()
	defer p.PolicyLock.RUnlock()

	for _, replica := range p.ReadyReplicas {
		logger.Log.Infof("Service: %s, Replica: %s, Requests: %d", replica.OwnerService, replica.IP, replica.NumberOfRequests)
	}
}

func (p *RequestLengthDispatchingPolicy) GetNumberOfRequests() map[string]int {
	logger.Log.Infof("Not implemented yet")
	return nil
}

func (p *RequestLengthDispatchingPolicy) GetLock() sync.Locker {
	return &p.PolicyLock
}

func (p *RequestLengthDispatchingPolicy) selectBestReplica(excludeReplica string, promptLength int) *types.Pod {
	var selectedReplica *types.Pod
	minCost := MaxInt

	for _, replica := range p.ReadyReplicas {
		if excludeReplica != "" && replica.IP == excludeReplica {
			continue
		}
		cost := p.computeCost(promptLength, replica)
		if cost < minCost {
			selectedReplica = replica
			minCost = cost
		}
	}

	logger.Log.Infof("current min cost: %d", minCost)

	return selectedReplica
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
