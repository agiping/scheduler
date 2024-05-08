package endpointwatcher

import (
	"context"
	"fmt"
	"log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"scheduler/scheduler/pkg/utils"
)

func WatchEndpoints() {
	namespace := "inference-service"
	serviceName := "chat-character-lite-online-sky"

	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatal(context.Background(), "Error getting in-cluster config: %v", err)
		return
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(context.Background(), "Error creating clientset: %v", err)
		return
	}

	watcher, err := clientset.CoreV1().Endpoints(namespace).Watch(context.TODO(), metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", serviceName),
	})
	if err != nil {
		log.Fatal(context.Background(), "Error watching endpoints: %v", err)
		return
	}
	defer watcher.Stop()

	var readyEndpoints []string
	for event := range watcher.ResultChan() {
		endpoints, ok := event.Object.(*v1.Endpoints)
		if !ok {
			continue
		}

		switch event.Type {
		case watch.Added, watch.Modified:
			// include: service is created; pod added, removed, or updated
			readyEndpoints = extractReadyEndpoints(endpoints)
		case watch.Deleted:
			readyEndpoints = []string{} // reset the list of ready endpoints if the endpoints are deleted
		}
		utils.ReadyEndpointsChan <- readyEndpoints
	}
}

func extractReadyEndpoints(endpoints *v1.Endpoints) []string {
	var endpointsList []string
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			// add the IP address and port of the pod to the list of ready endpoints
			// only if the target reference is a pod
			if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
				for _, port := range subset.Ports {
					endpointsList = append(endpointsList, fmt.Sprintf("%s:%d", address.IP, port.Port))
				}
			}
		}
	}
	return endpointsList
}
