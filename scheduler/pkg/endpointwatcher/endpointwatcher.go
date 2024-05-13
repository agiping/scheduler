package endpointwatcher

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"scheduler/scheduler/pkg/config"
	"scheduler/scheduler/pkg/logger"
	"scheduler/scheduler/pkg/utils"
)

func WatchEndpoints(cfg *config.SchedulerConfig) {
	namespace := cfg.Namespace
	serviceName := cfg.ServiceName

	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Log.Fatal(context.Background(), "Error getting in-cluster config: %v", err)
		return
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.Log.Fatal(context.Background(), "Error creating clientset: %v", err)
		return
	}

	for {
		watcher, err := clientset.CoreV1().Endpoints(namespace).Watch(context.TODO(), metav1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", serviceName),
		})
		if err != nil {
			logger.Log.Errorf("Error watching endpoints: %v", err)
			time.Sleep(time.Second * 2)
			continue
		}
		processEvents(watcher)
		logger.Log.Info("Watcher has stopped unexpectedly, restarting watcher...")
		// wait for a few seconds before restarting the watcher
		time.Sleep(time.Second * 5)
	}
}

func processEvents(watcher watch.Interface) {
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
			logger.Log.Errorf("Endpoints %s/%s has been deleted", endpoints.Namespace, endpoints.Name)
			readyEndpoints = []string{} // reset the list of ready endpoints if the endpoints are deleted
		}
		logger.Log.Errorf("Sending ready endpoints: %v", readyEndpoints)
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
