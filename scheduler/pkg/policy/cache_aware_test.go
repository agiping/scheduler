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
