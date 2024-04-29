package policy

import (
	"log"
	"sync"
	"time"

	"scheduler/scheduler/pkg/metrics"
	"scheduler/scheduler/pkg/types"
)

const (
	// The maximum queue size threshold to be considered as high pressure
	QHigh = 8
	// The minimum queue size threshold to be considered as low pressure
	QLow = 2
	// Cache replication control interval in seconds.
	// We control the size of the PodSet to prevent the number of replicas
	// that hold the cache data of a single sessionID from growing too large.
	PodSetSizeControlInterval = 120 * time.Second
	// Perform PodSet shrinking only if the size exceeds this threshold.
	PodSetSizeThreshold = 3
)

type CacheAwarePolicy struct {
	ReadyReplicas []*types.Pod
	PodSet        map[string][]*types.Pod // key: sessionID, value: list of pods
	LastModified  map[string]time.Time    // key: sessionID, value: last modified time of PodSet
	PoLock        sync.RWMutex
}

func NewCacheAwarePolicy() *CacheAwarePolicy {
	return &CacheAwarePolicy{
		PodSet:       make(map[string][]*types.Pod),
		LastModified: make(map[string]time.Time),
	}
}

func (cp *CacheAwarePolicy) SetReadyReplicas(replicas []string) {
	cp.PoLock.Lock()
	defer cp.PoLock.Unlock()

	newReplicaMap := make(map[string]*types.Pod)
	for _, podip := range replicas {
		newReplicaMap[podip] = &types.Pod{IP: podip}
	}

	updatedReplicas := []*types.Pod{}
	for _, pod := range cp.ReadyReplicas {
		if _, exists := newReplicaMap[pod.IP]; exists {
			updatedReplicas = append(updatedReplicas, pod)
			delete(newReplicaMap, pod.IP)
		} else {
			// Handle removed pods from PodSet
			cp.updatePodSet(pod)
		}
	}

	// Add new replicas scaled up by autoscaler
	for _, pod := range newReplicaMap {
		updatedReplicas = append(updatedReplicas, pod)
	}

	// Replace the old slice with the updated one
	cp.ReadyReplicas = updatedReplicas
}

// For those pods scaled down by autoscaler, updatePodSet removes them from the PodSet.
func (cp *CacheAwarePolicy) updatePodSet(removedPod *types.Pod) {
	for sessionID, pods := range cp.PodSet {
		var updatedPods []*types.Pod
		// podRemoved indicates whether the removedPod is found in the PodSet.
		podRemoved := false

		for _, pod := range pods {
			if pod.IP != removedPod.IP {
				updatedPods = append(updatedPods, pod)
			} else {
				podRemoved = true
				break
			}
		}

		// Only update the PodSet and LastModified if the removedPod is found.
		if podRemoved {
			cp.PodSet[sessionID] = append(updatedPods, pods[len(updatedPods)+1:]...)
			cp.LastModified[sessionID] = time.Now()
		}
	}
}

func (cp *CacheAwarePolicy) SelectReplica(request *types.InferRequest) string {
	// TODO(Ping Zhang): Abstract out the request validation logic.
	if request == nil {
		log.Print("Invalid request body: nil")
		return ""
	}

	requestBody := request.Body

	var selectedPod *types.Pod
	var requestType string
	cp.PoLock.RLock()
	if requestBody.StructInput.SessionID == "" {
		// Stateless request handling
		requestType = "STATELESS"
		selectedPod = cp.selectReplicaForStateless()
	} else {
		// Stateful request handling with session cache
		requestType = "STATEFUL"
		selectedPod = cp.selectReplicaForStateful(requestBody.StructInput.SessionID)
	}
	cp.PoLock.RUnlock()

	if selectedPod == nil {
		return ""
	}

	selectedPod.NumberOfRequests++
	// TODO (Ping Zhang): Only record requestsID to avoid printing sensitive information and large request body.
	log.Printf("Selected replica %s for [%s] request %v\n", selectedPod.IP, requestType, requestBody)
	return selectedPod.IP
}

func (cp *CacheAwarePolicy) selectReplicaForStateless() *types.Pod {
	var minPod *types.Pod
	// Max int
	// TODO(Ping Zhang): The bellow code is not clear, refactor it.
	// Finding the minPod from those that do not reject stateless requests
	// seems equal to finding the minPod from all pods,
	// since those pods that reject stateless requests almost always have a larger number of requests.
	// The only difference is that the former one may have a smaller number of pods to iterate.
	// Let's take this now as a temporary solution.
	minRequests := int(^uint(0) >> 1)
	for _, pod := range cp.ReadyReplicas {
		if pod.RejectStateless {
			continue
		}
		if pod.NumberOfRequests < minRequests {
			minRequests = pod.NumberOfRequests
			minPod = pod
		}
	}

	// If all pods were overloaded, fall back to selection from global pool
	if minPod == nil {
		// TODO(Ping Zhang): Trigger a event for scale up.
		// Reminder: Avoid repeated scale up events.
		log.Print("All pods are overloaded, We may proactively trigger a event for autoscaler to scale up.")
		minPod = findMinPod(cp.ReadyReplicas)
	}

	return minPod
}

func (cp *CacheAwarePolicy) selectReplicaForStateful(sessionID string) *types.Pod {
	pods, exists := cp.PodSet[sessionID]
	if !exists || len(pods) == 0 {
		minPod := cp.selectReplicaForStateless()
		// Since there's no entry for this sessionID or it's empty, create or update directly
		cp.PodSet[sessionID] = []*types.Pod{minPod}
		// Update last modified timestamp for this sessionID
		cp.LastModified[sessionID] = time.Now()
		return minPod
	}

	minPod := findMinPod(pods)
	maxPod := findMaxPod(pods)

	log.Printf("minPod: %v, maxPod: %v", minPod, maxPod)

	/**
	 * Overload Prevention: Identifies high load in pods of PodSet[sessionID].
	 * A pod is marked as 'highly overloaded' if its queue size exceeds twice the threshold QHigh.
	 * When detected, all pods in the set activate the 'RejectStateless' flag,
	 * preventing them from processing stateless requests.
	 * This aims to ensure a high hit rate of the existing SessionCache data.
	 */
	if minPod.TgiQueueSize >= 2*QHigh {
		for _, pod := range pods {
			pod.RejectStateless = true
		}
	}

	/**
	 * Cache Replication: Handles pod overload by replicate cache on a new instance.
	 * A pod is considered 'overloaded' if its queue size surpasses the threshold QHigh.
	 * To manage this, the globally minimal pod is added to the PodSet for the current sessionID,
	 * resulting in cache replication.
	 */
	if minPod.TgiQueueSize >= QHigh {
		globalMinPod := findMinPod(cp.ReadyReplicas)
		if globalMinPod.IP != minPod.IP {
			log.Printf("Replicating cache for session %s from %v to %v",
				sessionID,
				cp.PodSet[sessionID],
				globalMinPod)
			cp.PodSet[sessionID] = append(cp.PodSet[sessionID], globalMinPod)
			cp.LastModified[sessionID] = time.Now()
			minPod = globalMinPod
		}
	}

	// Shrink the PodSet if needed
	cp.shrinkCacheReplicationIfNeeded(sessionID, maxPod)

	return minPod
}

// shrinkCacheReplicationIfNeeded performs necessary shrinking of the PodSet.
// After maxPod is removed from podSet[r.session_id], it will no longer receive requests for r.session_id in the short term.
// Subsequent cache release is managed by the tgi LRU.
func (cp *CacheAwarePolicy) shrinkCacheReplicationIfNeeded(sessionID string, maxPod *types.Pod) {
	if len(cp.PodSet[sessionID]) >= PodSetSizeThreshold {
		if time.Since(cp.LastModified[sessionID]) >= PodSetSizeControlInterval {
			newPodSet := []*types.Pod{}
			for _, p := range cp.PodSet[sessionID] {
				if p.IP != maxPod.IP {
					newPodSet = append(newPodSet, p)
				}
			}
			cp.PodSet[sessionID] = newPodSet
			cp.LastModified[sessionID] = time.Now()
		}
	}
}

func (cp *CacheAwarePolicy) UpdateAfterResponse(podIP string) {
	cp.PoLock.Lock()
	defer cp.PoLock.Unlock()

	for _, pod := range cp.ReadyReplicas {
		if pod.IP == podIP {
			pod.NumberOfRequests--
			if pod.RejectStateless && pod.TgiQueueSize < QHigh {
				pod.RejectStateless = false
			}
			return
		}
	}
}

// UpdateTgiQueueSize updates the TGI queue size for each pod in the ReadyReplicas,
// where the queue size is fetched from the TGI instance by Collector.
// TODO(Ping Zhang): Optimize the performance.
// When QPS is high, both the read operation and write operation for pod.TgiQueueSize
// would be at a high rate, which may lead to a performance bottleneck.
// e.g., Consider Register & Notify mode.
func (cp *CacheAwarePolicy) UpdateTgiQueueSize(tgiQ *sync.Map) {
	cp.PoLock.Lock()
	defer cp.PoLock.Unlock()

	for _, pod := range cp.ReadyReplicas {
		if queueState, exists := tgiQ.Load(pod.IP); exists {
			pod.TgiQueueSize = queueState.(metrics.TgiMetrics).QueueSize
		} else {
			log.Printf("Queue size of pod %s is not updated", pod.IP)
		}
	}
}

// TODO (Ping Zhang): We may need to shuffle the ReadyReplicas to avoid the same pod being selected repeatedly.
// findMinPod returns the pod with the minimum number of requests from the given list of pods.
func findMinPod(pods []*types.Pod) *types.Pod {
	var minPod *types.Pod
	minRequests := int(^uint(0) >> 1)

	for _, pod := range pods {
		if pod.NumberOfRequests < minRequests {
			minRequests = pod.NumberOfRequests
			minPod = pod
		}
	}
	return minPod
}

// findMaxPod returns the pod with the maximum number of requests from the given list of pods.
func findMaxPod(pods []*types.Pod) *types.Pod {
	var maxPod *types.Pod
	maxRequests := -1

	for _, pod := range pods {
		if pod.NumberOfRequests > maxRequests {
			maxRequests = pod.NumberOfRequests
			maxPod = pod
		}
	}
	return maxPod
}

func (cp *CacheAwarePolicy) GetLock() sync.Locker {
	return &cp.PoLock
}

func (cp *CacheAwarePolicy) GetReadyReplicas() []*types.Pod {
	return cp.ReadyReplicas
}
