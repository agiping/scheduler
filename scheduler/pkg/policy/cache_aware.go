package policy

import (
	"encoding/json"
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

// selectLeastLoadedPod selects the pod with the least number of requests.
func (cp *CacheAwarePolicy) SelectReplica(request *http.Request) *string {
	var minPod *Pod
	minRequests := int(^uint(0) >> 1) // Max int
	for _, p := range pods {
		if p.AcceptStateless && p.NumberOfRequests < minRequests {
			minRequests = p.NumberOfRequests
			minPod = p
		}
	}
	return minPod
}

// checkForRebalance checks if rebalancing is needed for the pod set.
func (cp *CacheAwarePolicy) CheckForRebalance(selectedPod *Pod, pods []*Pod) bool {
	for _, p := range pods {
		if p.TgiQueueSize > 2*QHigh {
			p.AcceptStateless = false
			return true
		}
	}
	return false
}

// shrinkIfNeeded performs shrinking of the pod set if needed.
func (cp *CacheAwarePolicy) ShrinkIfNeeded(sessionID string) {
	if time.Since(lastModified[sessionID]) > PodSetSizeControlInterval && len(podSet[sessionID]) > 1 {
		// Remove the pod with the highest number of requests
		var maxPod *Pod
		maxRequests := 0
		for _, p := range podSet[sessionID] {
			if p.NumberOfRequests > maxRequests {
				maxRequests = p.NumberOfRequests
				maxPod = p
			}
		}
		// Remove maxPod from podSet
		newPodSet := []*Pod{}
		for _, p := range podSet[sessionID] {
			if p != maxPod {
				newPodSet = append(newPodSet, p)
			}
		}
		podSet[sessionID] = newPodSet
		lastModified[sessionID] = time.Now()
	}
}

// func main() {
// 	http.HandleFunc("/generate", HandleRequest)
// 	log.Fatal(http.ListenAndServe(":80", nil))
// }

// HandleRequest handles incoming requests.
func HandleRequest(w http.ResponseWriter, req *http.Request) {
	var input struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var selectedPod *Pod
	mutex.Lock()
	if input.SessionID == "" {
		// Stateless request handling
		selectedPod = selectLeastLoadedPod(allPods)
	} else {
		// Stateful request handling with session affinity
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
	}
	mutex.Unlock()

	// Simulate sending request to the selected pod
	selectedPod.Mutex.Lock()
	selectedPod.NumberOfRequests++
	selectedPod.Mutex.Unlock()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Request handled by pod: " + selectedPod.IP))
}
