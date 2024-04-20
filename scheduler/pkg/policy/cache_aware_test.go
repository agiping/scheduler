package policy

import (
	"reflect"
	"testing"
	"time"
)

func TestCacheAwarePolicy_SetReadyReplicas(t *testing.T) {
	cp := &CacheAwarePolicy{
		ReadyReplicas: []*Pod{
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

func TestCacheAwarePolicy_cleanPodSet(t *testing.T) {
	cp := &CacheAwarePolicy{
		PodSet: map[string][]*Pod{
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

	removedPod := &Pod{IP: "2.2.2.2"}

	cp.cleanPodSet(removedPod)

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

func TestSelectReplicaForStateless(t *testing.T) {
	tests := []struct {
		name          string
		readyReplicas []*Pod
		expectedIP    string
	}{
		{
			name: "all pods accept stateless",
			readyReplicas: []*Pod{
				{IP: "1.1.1.1", RejectStateless: false, NumberOfRequests: 10},
				{IP: "2.2.2.2", RejectStateless: false, NumberOfRequests: 5},
				{IP: "3.3.3.3", RejectStateless: false, NumberOfRequests: 15},
			},
			expectedIP: "2.2.2.2", // Should select the pod with the minimum number of requests
		},
		{
			name: "some pods accept stateless",
			readyReplicas: []*Pod{
				{IP: "1.1.1.1", RejectStateless: false, NumberOfRequests: 10},
				{IP: "2.2.2.2", RejectStateless: false, NumberOfRequests: 5},
				{IP: "3.3.3.3", RejectStateless: true, NumberOfRequests: 20},
			},
			expectedIP: "2.2.2.2", // Should select the pod that accepts stateless and has the minimum number of requests
		},
		{
			name: "all pods reject stateless",
			readyReplicas: []*Pod{
				{IP: "1.1.1.1", RejectStateless: true, NumberOfRequests: 23},
				{IP: "2.2.2.2", RejectStateless: true, NumberOfRequests: 20},
				{IP: "3.3.3.3", RejectStateless: true, NumberOfRequests: 24},
			},
			expectedIP: "2.2.2.2", // Should select the pod with the minimum number of requests
		},
		{
			name:          "no ready replicas",
			readyReplicas: []*Pod{},
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

func TestShrinkCacheReplicationIfNeeded(t *testing.T) {
	tests := []struct {
		name           string
		podSet         map[string][]*Pod
		lastModified   map[string]time.Time
		sessionID      string
		maxPod         *Pod
		expectedPodSet map[string][]*Pod
	}{
		{
			name: "shrink not needed: size < threshold",
			podSet: map[string][]*Pod{
				"session1": {
					{IP: "1.1.1.1"},
					{IP: "2.2.2.2"},
				},
			},
			lastModified: map[string]time.Time{
				"session1": time.Now(),
			},
			sessionID: "session1",
			maxPod:    &Pod{IP: "2.2.2.2"},
			expectedPodSet: map[string][]*Pod{
				"session1": {
					{IP: "1.1.1.1"},
					{IP: "2.2.2.2"},
				},
			},
		},
		{
			name: "shrink not needed: size >= threshold but time < interval",
			podSet: map[string][]*Pod{
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
			maxPod:    &Pod{IP: "2.2.2.2"},
			expectedPodSet: map[string][]*Pod{
				"session1": {
					{IP: "1.1.1.1"},
					{IP: "2.2.2.2"},
					{IP: "3.3.3.3"},
				},
			},
		},
		{
			name: "shrink needed: size >= threshold and time >= interval",
			podSet: map[string][]*Pod{
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
			maxPod:    &Pod{IP: "2.2.2.2"},
			expectedPodSet: map[string][]*Pod{
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

func TestFindMinPod(t *testing.T) {
	pods := []*Pod{
		{IP: "1.1.1.1", NumberOfRequests: 10},
		{IP: "2.2.2.2", NumberOfRequests: 5},
		{IP: "3.3.3.3", NumberOfRequests: 20},
	}

	minPod := findMinPod(pods)

	if minPod.IP != "2.2.2.2" {
		t.Errorf("Expected min pod IP to be 2.2.2.2, got %s", minPod.IP)
	}
}

func TestFindMaxPod(t *testing.T) {
	pods := []*Pod{
		{IP: "1.1.1.1", NumberOfRequests: 10},
		{IP: "2.2.2.2", NumberOfRequests: 5},
		{IP: "3.3.3.3", NumberOfRequests: 20},
	}

	maxPod := findMaxPod(pods)

	if maxPod.IP != "3.3.3.3" {
		t.Errorf("Expected max pod IP to be 3.3.3.3, got %s", maxPod.IP)
	}
}
