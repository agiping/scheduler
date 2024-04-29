package policy

import (
	"reflect"
	"scheduler/scheduler/pkg/types"
	"sync"
	"testing"
	"time"
)

func TestCacheAwarePolicy_SetReadyReplicas(t *testing.T) {
	cp := &CacheAwarePolicy{
		ReadyReplicas: []*types.Pod{
			{IP: "1.1.1.1"},
			{IP: "2.2.2.2"},
		},
	}

	newReplicas := []string{"2.2.2.2", "3.3.3.3"}

	cp.SetReadyReplicas(newReplicas)

	if len(cp.ReadyReplicas) != 2 {
		t.Errorf("Expected 2 replicas, got %d", len(cp.ReadyReplicas))
	}

	expectedIPs := []string{"2.2.2.2", "3.3.3.3"}
	actualIPs := []string{cp.ReadyReplicas[0].IP, cp.ReadyReplicas[1].IP}

	if !reflect.DeepEqual(expectedIPs, actualIPs) {
		t.Errorf("Expected IPs %v, got %v", expectedIPs, actualIPs)
	}
}

// func TestCacheAwarePolicy_SelectReplica(t *testing.T) {
// 	tests := []struct {
// 		name        string
// 		requestBody string
// 		expectedIP  string
// 	}{
// 		{
// 			name:        "invalid request",
// 			requestBody: `{"invalid_json":}`,
// 			expectedIP:  "",
// 		},
// 		{
// 			name:        "stateless request",
// 			requestBody: `{}`,
// 			expectedIP:  "",
// 		},
// 		{
// 			name:        "stateful request",
// 			requestBody: `{"session_id": "session1"}`,
// 			expectedIP:  "",
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			cp := NewCacheAwarePolicy()

// 			request := &http.Request{
// 				Body: io.NopCloser(strings.NewReader(tt.requestBody)),
// 			}

// 			ip := cp.SelectReplica(request)

// 			if ip != tt.expectedIP {
// 				t.Errorf("Expected IP to be %s, got %s", tt.expectedIP, ip)
// 			}
// 		})
// 	}
// }

func TestCacheAwarePolicy_updatePodSet(t *testing.T) {
	cp := &CacheAwarePolicy{
		PodSet: map[string][]*types.Pod{
			"session1": {
				{IP: "1.1.1.1"},
				{IP: "2.2.2.2"},
			},
			"session2": {
				{IP: "2.2.2.2"},
				{IP: "3.3.3.3"},
			},
		},
		LastModified: map[string]time.Time{
			"session1": time.Now().Add(-time.Hour),
			"session2": time.Now().Add(-time.Hour),
		},
	}

	removedPod := &types.Pod{IP: "2.2.2.2"}

	cp.updatePodSet(removedPod)

	if len(cp.PodSet["session1"]) != 1 || cp.PodSet["session1"][0].IP != "1.1.1.1" {
		t.Errorf("Expected session1 to contain only pod 1.1.1.1, got %v", cp.PodSet["session1"])
	}

	if len(cp.PodSet["session2"]) != 1 || cp.PodSet["session2"][0].IP != "3.3.3.3" {
		t.Errorf("Expected session2 to contain only pod 3.3.3.3, got %v", cp.PodSet["session2"])
	}

	if time.Since(cp.LastModified["session1"]) > 2*time.Second {
		t.Errorf("Expected LastModified of session1 to be updated, but it was not")
	}

	if time.Since(cp.LastModified["session2"]) > 2*time.Second {
		t.Errorf("Expected LastModified of session2 to be updated, but it was not")
	}
}

func TestCacheAwarePolicy_SelectReplicaForStateless(t *testing.T) {
	tests := []struct {
		name          string
		readyReplicas []*types.Pod
		expectedIP    string
	}{
		{
			name: "all pods accept stateless",
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", RejectStateless: false, NumberOfRequests: 10},
				{IP: "2.2.2.2", RejectStateless: false, NumberOfRequests: 5},
				{IP: "3.3.3.3", RejectStateless: false, NumberOfRequests: 15},
			},
			expectedIP: "2.2.2.2", // Should select the pod with the minimum number of requests
		},
		{
			name: "some pods accept stateless",
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", RejectStateless: false, NumberOfRequests: 10},
				{IP: "2.2.2.2", RejectStateless: false, NumberOfRequests: 5},
				{IP: "3.3.3.3", RejectStateless: true, NumberOfRequests: 20},
			},
			expectedIP: "2.2.2.2", // Should select the pod that accepts stateless and has the minimum number of requests
		},
		{
			name: "all pods reject stateless",
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", RejectStateless: true, NumberOfRequests: 23},
				{IP: "2.2.2.2", RejectStateless: true, NumberOfRequests: 20},
				{IP: "3.3.3.3", RejectStateless: true, NumberOfRequests: 24},
			},
			expectedIP: "2.2.2.2", // Should select the pod with the minimum number of requests
		},
		{
			name:          "no ready replicas",
			readyReplicas: []*types.Pod{},
			expectedIP:    "", // Should return nil when there are no ready replicas
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := &CacheAwarePolicy{
				ReadyReplicas: tt.readyReplicas,
			}

			minPod := cp.selectReplicaForStateless()

			if minPod != nil && minPod.IP != tt.expectedIP {
				t.Errorf("Expected min pod IP to be %s, got %s", tt.expectedIP, minPod.IP)
			} else if minPod == nil && tt.expectedIP != "" {
				t.Errorf("Expected min pod IP to be %s, got nil", tt.expectedIP)
			}
		})
	}
}

func TestCacheAwarePolicy_SelectReplicaForStateful(t *testing.T) {
	tests := []struct {
		name           string
		podSet         map[string][]*types.Pod
		readyReplicas  []*types.Pod
		lastModified   map[string]time.Time
		sessionID      string
		expectedPodSet map[string][]*types.Pod
	}{
		{
			name: "no existing session",
			podSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1", NumberOfRequests: 10},
					{IP: "2.2.2.2", NumberOfRequests: 15},
				},
			},
			lastModified: map[string]time.Time{
				"session1": time.Now(),
			},
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", NumberOfRequests: 10},
				{IP: "2.2.2.2", NumberOfRequests: 15},
				{IP: "3.3.3.3", NumberOfRequests: 5},
				{IP: "4.4.4.4", NumberOfRequests: 20},
			},
			sessionID: "session2",
			expectedPodSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1", NumberOfRequests: 10},
					{IP: "2.2.2.2", NumberOfRequests: 15},
				},
				"session2": {
					{IP: "3.3.3.3", NumberOfRequests: 5},
				},
			},
		},
		{
			name: "existing session with no overload",
			podSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1", NumberOfRequests: 10, TgiQueueSize: 5},
					{IP: "2.2.2.2", NumberOfRequests: 15, TgiQueueSize: 10},
				},
			},
			lastModified: map[string]time.Time{
				"session1": time.Now(),
			},
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", NumberOfRequests: 10, TgiQueueSize: 5},
				{IP: "2.2.2.2", NumberOfRequests: 15, TgiQueueSize: 10},
				{IP: "3.3.3.3", NumberOfRequests: 5},
				{IP: "4.4.4.4", NumberOfRequests: 20},
			},
			sessionID: "session1",
			expectedPodSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1", NumberOfRequests: 10, TgiQueueSize: 5},
					{IP: "2.2.2.2", NumberOfRequests: 15, TgiQueueSize: 10},
				},
			},
		},
		{
			name: "existing session with overload",
			podSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1", NumberOfRequests: 10, TgiQueueSize: QHigh},
					{IP: "2.2.2.2", NumberOfRequests: 15, TgiQueueSize: QHigh},
				},
			},
			lastModified: map[string]time.Time{
				"session1": time.Now().Add(-2 * PodSetSizeControlInterval),
			},
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", NumberOfRequests: 10, TgiQueueSize: QHigh},
				{IP: "2.2.2.2", NumberOfRequests: 15, TgiQueueSize: QHigh},
				{IP: "3.3.3.3", NumberOfRequests: 5},
				{IP: "4.4.4.4", NumberOfRequests: 20},
			},
			sessionID: "session1",
			expectedPodSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1", NumberOfRequests: 10, TgiQueueSize: QHigh},
					{IP: "2.2.2.2", NumberOfRequests: 15, TgiQueueSize: QHigh},
					{IP: "3.3.3.3", NumberOfRequests: 5},
				},
			},
		},
		{
			name: "existing session with highly overload",
			podSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1", NumberOfRequests: 10, TgiQueueSize: 2 * QHigh, RejectStateless: false},
					{IP: "2.2.2.2", NumberOfRequests: 15, TgiQueueSize: 2 * QHigh, RejectStateless: false},
				},
			},
			lastModified: map[string]time.Time{
				"session1": time.Now().Add(-2 * PodSetSizeControlInterval),
			},
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", NumberOfRequests: 10, TgiQueueSize: 2 * QHigh},
				{IP: "2.2.2.2", NumberOfRequests: 15, TgiQueueSize: 2 * QHigh},
				{IP: "3.3.3.3", NumberOfRequests: 5},
				{IP: "4.4.4.4", NumberOfRequests: 20},
			},
			sessionID: "session1",
			expectedPodSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1", NumberOfRequests: 10, TgiQueueSize: 2 * QHigh, RejectStateless: true},
					{IP: "2.2.2.2", NumberOfRequests: 15, TgiQueueSize: 2 * QHigh, RejectStateless: true},
					{IP: "3.3.3.3", NumberOfRequests: 5},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := &CacheAwarePolicy{
				PodSet:        tt.podSet,
				ReadyReplicas: tt.readyReplicas,
				LastModified:  tt.lastModified,
			}

			cp.selectReplicaForStateful(tt.sessionID)

			if !reflect.DeepEqual(cp.PodSet, tt.expectedPodSet) {
				t.Errorf("Expected PodSet to be %v, got %v", tt.expectedPodSet, cp.PodSet)
			}
		})
	}
}

func TestCacheAwarePolicy_shrinkCacheReplicationIfNeeded(t *testing.T) {
	tests := []struct {
		name           string
		podSet         map[string][]*types.Pod
		lastModified   map[string]time.Time
		sessionID      string
		maxPod         *types.Pod
		expectedPodSet map[string][]*types.Pod
	}{
		{
			name: "shrink not needed: size < threshold",
			podSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1"},
					{IP: "2.2.2.2"},
				},
			},
			lastModified: map[string]time.Time{
				"session1": time.Now(),
			},
			sessionID: "session1",
			maxPod:    &types.Pod{IP: "2.2.2.2"},
			expectedPodSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1"},
					{IP: "2.2.2.2"},
				},
			},
		},
		{
			name: "shrink not needed: size >= threshold but time < interval",
			podSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1"},
					{IP: "2.2.2.2"},
					{IP: "3.3.3.3"},
				},
			},
			lastModified: map[string]time.Time{
				"session1": time.Now(),
			},
			sessionID: "session1",
			maxPod:    &types.Pod{IP: "2.2.2.2"},
			expectedPodSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1"},
					{IP: "2.2.2.2"},
					{IP: "3.3.3.3"},
				},
			},
		},
		{
			name: "shrink needed: size >= threshold and time >= interval",
			podSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1"},
					{IP: "2.2.2.2"},
					{IP: "3.3.3.3"},
				},
			},
			lastModified: map[string]time.Time{
				"session1": time.Now().Add(-2 * PodSetSizeControlInterval),
			},
			sessionID: "session1",
			maxPod:    &types.Pod{IP: "2.2.2.2"},
			expectedPodSet: map[string][]*types.Pod{
				"session1": {
					{IP: "1.1.1.1"},
					{IP: "3.3.3.3"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := &CacheAwarePolicy{
				PodSet:       tt.podSet,
				LastModified: tt.lastModified,
			}

			cp.shrinkCacheReplicationIfNeeded(tt.sessionID, tt.maxPod)

			if !reflect.DeepEqual(cp.PodSet, tt.expectedPodSet) {
				t.Errorf("Expected PodSet to be %v, got %v", tt.expectedPodSet, cp.PodSet)
			}
		})
	}
}

func TestCacheAwarePolicy_UpdateAfterResponse(t *testing.T) {
	tests := []struct {
		name             string
		podIP            string
		readyReplicas    []*types.Pod
		expectedReplicas []*types.Pod
	}{
		{
			name:  "pod not found",
			podIP: "5.5.5.5",
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", NumberOfRequests: 10},
				{IP: "2.2.2.2", NumberOfRequests: 15},
			},
			expectedReplicas: []*types.Pod{
				{IP: "1.1.1.1", NumberOfRequests: 10},
				{IP: "2.2.2.2", NumberOfRequests: 15},
			},
		},
		{
			name:  "pod found, number of requests not less than QHigh",
			podIP: "1.1.1.1",
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", NumberOfRequests: 10, RejectStateless: true, TgiQueueSize: QHigh + 1},
				{IP: "2.2.2.2", NumberOfRequests: 15},
			},
			expectedReplicas: []*types.Pod{
				{IP: "1.1.1.1", NumberOfRequests: 9, RejectStateless: true, TgiQueueSize: QHigh + 1},
				{IP: "2.2.2.2", NumberOfRequests: 15},
			},
		},
		{
			name:  "pod found, number of requests less than QHigh",
			podIP: "1.1.1.1",
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", NumberOfRequests: 10, RejectStateless: true, TgiQueueSize: QHigh - 1},
				{IP: "2.2.2.2", NumberOfRequests: 15},
			},
			expectedReplicas: []*types.Pod{
				{IP: "1.1.1.1", NumberOfRequests: 9, RejectStateless: false, TgiQueueSize: QHigh - 1},
				{IP: "2.2.2.2", NumberOfRequests: 15},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := &CacheAwarePolicy{
				ReadyReplicas: tt.readyReplicas,
			}

			cp.UpdateAfterResponse(tt.podIP)

			if !reflect.DeepEqual(cp.ReadyReplicas, tt.expectedReplicas) {
				t.Errorf("Expected ReadyReplicas to be %v, got %v", tt.expectedReplicas, cp.ReadyReplicas)
			}
		})
	}
}

func TestUpdateTgiQueueSize(t *testing.T) {
	tests := []struct {
		name             string
		tgiQ             *sync.Map
		readyReplicas    []*types.Pod
		expectedReplicas []*types.Pod
	}{
		{
			name: "update queue size",
			tgiQ: func() *sync.Map {
				m := &sync.Map{}
				m.Store("1.1.1.1", 5)
				m.Store("2.2.2.2", 10)
				return m
			}(),
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", TgiQueueSize: 0},
				{IP: "2.2.2.2", TgiQueueSize: 0},
			},
			expectedReplicas: []*types.Pod{
				{IP: "1.1.1.1", TgiQueueSize: 5},
				{IP: "2.2.2.2", TgiQueueSize: 10},
			},
		},
		{
			name: "no update",
			tgiQ: func() *sync.Map {
				m := &sync.Map{}
				m.Store("3.3.3.3", 5)
				m.Store("4.4.4.4", 10)
				return m
			}(),
			readyReplicas: []*types.Pod{
				{IP: "1.1.1.1", TgiQueueSize: 0},
				{IP: "2.2.2.2", TgiQueueSize: 0},
			},
			expectedReplicas: []*types.Pod{
				{IP: "1.1.1.1", TgiQueueSize: 0},
				{IP: "2.2.2.2", TgiQueueSize: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := &CacheAwarePolicy{
				ReadyReplicas: tt.readyReplicas,
			}

			cp.UpdateTgiQueueSize(tt.tgiQ)

			for i, pod := range cp.ReadyReplicas {
				if pod.IP != tt.expectedReplicas[i].IP || pod.TgiQueueSize != tt.expectedReplicas[i].TgiQueueSize {
					t.Errorf("Expected pod to be %v, got %v", tt.expectedReplicas[i], pod)
				}
			}
		})
	}
}

func TestCacheAwarePolicy_FindMinPod(t *testing.T) {
	pods := []*types.Pod{
		{IP: "1.1.1.1", NumberOfRequests: 10},
		{IP: "2.2.2.2", NumberOfRequests: 5},
		{IP: "3.3.3.3", NumberOfRequests: 20},
	}

	minPod := findMinPod(pods)

	if minPod.IP != "2.2.2.2" {
		t.Errorf("Expected min pod IP to be 2.2.2.2, got %s", minPod.IP)
	}
}

func TestCacheAwarePolicy_FindMaxPod(t *testing.T) {
	pods := []*types.Pod{
		{IP: "1.1.1.1", NumberOfRequests: 10},
		{IP: "2.2.2.2", NumberOfRequests: 5},
		{IP: "3.3.3.3", NumberOfRequests: 20},
	}

	maxPod := findMaxPod(pods)

	if maxPod.IP != "3.3.3.3" {
		t.Errorf("Expected max pod IP to be 3.3.3.3, got %s", maxPod.IP)
	}
}
