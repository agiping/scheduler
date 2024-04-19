package policy

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	// The maximum queue size threshold
	QHigh = 8
	// The minimum queue size threshold
	QLow = 0
	// Cache replication control interval in seconds.
	// We control the size of the PodSet to prevent the number of replicas
	// that hold the cache data of a single sessionID from growing too large.
	PodSetSizeControlInterval = 120 * time.Second
)

type Pod struct {
	IP               string // IP address of the pod: ip:port
	RejectStateless  bool   // Whether the pod rejects stateless requests
	NumberOfRequests int    // Number of requests handled by the pod
	TgiQueueSize     int    // The queue size of the TGI instance
}

type CacheAwarePolicy struct {
	ReadyReplicas []*Pod
	PodSet        map[string][]*Pod    // key: sessionID, value: list of pods
	LastModified  map[string]time.Time // key: sessionID, value: last modified time of PodSet
	Mutex         sync.RWMutex
}

func NewCacheAwarePolicy() *CacheAwarePolicy {
	return &CacheAwarePolicy{
		PodSet:       make(map[string][]*Pod),
		LastModified: make(map[string]time.Time),
	}
}

func (cp *CacheAwarePolicy) SetReadyReplicas(replicas []string) {
	cp.Mutex.Lock()
	defer cp.Mutex.Unlock()

	newReplicaMap := make(map[string]*Pod)
	for _, podip := range replicas {
		newReplicaMap[podip] = &Pod{IP: podip}
	}

	updatedReplicas := []*Pod{}
	for _, pod := range cp.ReadyReplicas {
		if _, exists := newReplicaMap[pod.IP]; exists {
			updatedReplicas = append(updatedReplicas, pod)
			delete(newReplicaMap, pod.IP)
		} else {
			// Handle removed pods from PodSet
			cp.cleanPodSet(pod)
		}
	}

	// Add new replicas scaled up by autoscaler
	for _, pod := range newReplicaMap {
		updatedReplicas = append(updatedReplicas, pod)
	}

	// Replace the old slice with the updated one
	cp.ReadyReplicas = updatedReplicas
}

// For those pods scaled down by autoscaler, cleanPodSet removes them from the PodSet.
func (cp *CacheAwarePolicy) cleanPodSet(removedPod *Pod) {
	cp.Mutex.Lock()
	defer cp.Mutex.Unlock()

	for sessionID, pods := range cp.PodSet {
		var updatedPods []*Pod
		// podRemoved indicates whether the removedPod is found in the PodSet.
		podRemoved := false

		for _, pod := range pods {
			if pod.IP != removedPod.IP {
				updatedPods = append(updatedPods, pod)
			} else {
				podRemoved = true
			}
		}

		// Only update the PodSet and LastModified if the removedPod is found.
		if podRemoved {
			cp.PodSet[sessionID] = updatedPods
			cp.LastModified[sessionID] = time.Now()
		}
	}
}

func (cp *CacheAwarePolicy) SelectReplica(request *http.Request) string {
	// TODO(Ping Zhang): Abstract out the request validation logic.
	var reqSession struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(request.Body).Decode(&reqSession); err != nil {
		log.Printf("Invalid request: %v", request)
		return ""
	}

	var selectedPod *Pod
	cp.Mutex.RLock()
	if reqSession.SessionID == "" {
		// Stateless request handling
		selectedPod = cp.selectReplicaForStateless()
	} else {
		// Stateful request handling with session cache
		selectedPod = cp.selectReplicaForStateful(reqSession.SessionID)
	}
	cp.Mutex.RUnlock()

	if selectedPod == nil {
		return ""
	}

	selectedPod.NumberOfRequests++
	log.Printf("Selected replica %s for request %v\n", selectedPod.IP, request)
	return selectedPod.IP
}

func (cp *CacheAwarePolicy) selectReplicaForStateless() *Pod {
	var minPod *Pod
	// Max int
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

func (cp *CacheAwarePolicy) selectReplicaForStateful(sessionID string) *Pod {
	pods, exists := cp.PodSet[sessionID]
	if !exists || len(pods) == 0 {
		minPod := cp.selectReplicaForStateless()
		// Since there's no entry for this sessionID or it's empty, create or update directly
		cp.PodSet[sessionID] = []*Pod{minPod}
		// Update last modified timestamp for this sessionID
		cp.LastModified[sessionID] = time.Now()
		return minPod
	}

	minPod := findMinPod(pods)
	maxPod := findMaxPod(pods)

	log.Printf("minPod: %v, maxPod: %v", minPod, maxPod)

	/***
	  I. Prevention Mode: Pod is highly overloaded.
	  If the queue size of the pod with the fewest requests exceeds twice the value of QHigh,
	  the pod is considered to be highly overloaded.
	  In such cases, set the RejectStateless flag to true for all pods within the pod set.
	  This action prevents the pod from being selected for processing stateless requests.
	  The goal is to maximize the utilization of the SessionCache data available on the pod,
	  thereby optimizing resource allocation and performance.
	  ***/
	if minPod.TgiQueueSize >= 2*QHigh {
		for _, pod := range pods {
			pod.RejectStateless = true
		}
		// do something
	}

	/***
	  II. Replication Mode: Pod is overloaded.
	  If the queue size of the pod with the fewest requests exceeds QHigh,
	  the pod is considered to be overloaded.
	  In such cases, we add a new pod into PodSet for current sessionID, which means
	  the cache would be replicated.
	  ***/
	if minPod.TgiQueueSize >= QHigh && minPod.TgiQueueSize < 2*QHigh {
		// do something
		log.Printf("do something")
	}

	/***
		pods, exists := podSet[input.SessionID]
		if !exists || len(pods) == 0 {
			selectedPod = selectLeastLoadedPod(allPods)
			podSet[input.SessionID] = []*Pod{selectedPod}
			lastModified[input.SessionID] = time.Now()
		} else {
			selectedPod = selectLeastLoadedPod(pods)
			if checkForRebalance(selectedPod, pods) {
				newPod := selectLeastLoadedPod(allPods)
				podSet[input.SessionID] = append(podSet[input.SessionID], newPod)
				selectedPod = newPod
			}
			shrinkIfNeeded(input.SessionID)
		}
	    ***/

	return minPod
}

// CheckForScaleCache checks if cache is needed for replication on new pod.
func (cp *CacheAwarePolicy) CheckForScaleCache(selectedPod *Pod, pods []*Pod) bool {
	for _, p := range pods {
		if p.TgiQueueSize > QHigh {
			p.RejectStateless = true
			return true
		}
	}
	return false
}

func (cp *CacheAwarePolicy) UpdateAfterResponse(podIP string) {
	cp.Mutex.Lock()
	defer cp.Mutex.Unlock()

	for _, pod := range cp.ReadyReplicas {
		if pod.IP == podIP {
			pod.NumberOfRequests--
			if pod.RejectStateless && pod.NumberOfRequests < QHigh {
				pod.RejectStateless = false
			}
			return
		}
	}
}

// ShrinkCacheReplicationIfNeeded performs shrinking of the pod set if needed.
func (cp *CacheAwarePolicy) ShrinkCacheReplicationIfNeeded(sessionID string) {
	if time.Since(cp.LastModified[sessionID]) > PodSetSizeControlInterval && len(cp.PodSet[sessionID]) > 1 {
		// Remove the pod with the highest number of requests
		var maxPod *Pod
		maxRequests := 0
		for _, p := range cp.PodSet[sessionID] {
			if p.NumberOfRequests > maxRequests {
				maxRequests = p.NumberOfRequests
				maxPod = p
			}
		}
		// Remove maxPod from podSet
		newPodSet := []*Pod{}
		for _, p := range cp.PodSet[sessionID] {
			if p != maxPod {
				newPodSet = append(newPodSet, p)
			}
		}
		cp.PodSet[sessionID] = newPodSet
		cp.LastModified[sessionID] = time.Now()
	}
}

// func main() {
// 	http.HandleFunc("/generate", HandleRequest)
// 	log.Fatal(http.ListenAndServe(":80", nil))
// }

// findMinPod returns the pod with the minimum number of requests from the given list of pods.
func findMinPod(pods []*Pod) *Pod {
	var minPod *Pod
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
func findMaxPod(pods []*Pod) *Pod {
	var maxPod *Pod
	maxRequests := -1

	for _, pod := range pods {
		if pod.NumberOfRequests > maxRequests {
			maxRequests = pod.NumberOfRequests
			maxPod = pod
		}
	}
	return maxPod
}
