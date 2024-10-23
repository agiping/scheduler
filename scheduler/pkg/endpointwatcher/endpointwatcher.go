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
	serviceNames := cfg.ServiceNames
	logger.Log.Info("WatchEndpoint config, namespace: ", namespace, ", serviceNames: ", serviceNames)

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

	for _, serviceName := range serviceNames {
		go func(numOfServices int, serviceName string) {
			for {
				watcher, err := clientset.CoreV1().Endpoints(namespace).Watch(context.TODO(), metav1.ListOptions{
					FieldSelector: fmt.Sprintf("metadata.name=%s", serviceName),
				})
				if err != nil {
					logger.Log.Errorf("Error watching endpoints for service %s: %v", serviceName, err)
					time.Sleep(time.Second * 2)
					continue
				}
				processEvents(watcher, serviceName, numOfServices)
				logger.Log.Infof("Watcher for service %s has stopped unexpectedly, restarting watcher...", serviceName)
				time.Sleep(time.Second * 5)
			}
		}(cfg.NumOfServices, serviceName)
	}
}

func processEvents(watcher watch.Interface, serviceName string, numOfServices int) {
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
			readyEndpoints = extractReadyEndpoints(endpoints, serviceName, numOfServices)
		case watch.Deleted:
			logger.Log.Errorf("Endpoints %s/%s has been deleted", endpoints.Namespace, endpoints.Name)
			readyEndpoints = []string{}
		}
		logger.Log.Debugf("Sending ready endpoints for service %s: %v", serviceName, readyEndpoints)
		logger.Log.Infof("Sending ready endpoints count for service %s: %d", serviceName, len(readyEndpoints))
		utils.ReadyEndpointsChan <- readyEndpoints
	}
}

func extractReadyEndpoints(endpoints *v1.Endpoints, serviceName string, numOfServices int) []string {
	var endpointsList []string
	var ep string
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
				for _, port := range subset.Ports {
					if numOfServices == 1 {
						ep = fmt.Sprintf("%s:%d", address.IP, port.Port) // podip:port
					} else {
						ep = fmt.Sprintf("%s-%s:%d", serviceName, address.IP, port.Port) // serviceName-podip:port
					}
					endpointsList = append(endpointsList, ep)
				}
			}
		}
	}
	return endpointsList
}

/*
TODO (Ping Zhang):
1. get pod from targetRef,
2. differentiate pod according to the pod's resource request, i.e., GPU count.

func extractReadyEndpoints(endpoints *v1.Endpoints, serviceName string) []string {
	var endpointsList []string
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
				pod, err := getPod(address.TargetRef.Namespace, address.TargetRef.Name)
				if err != nil {
					logger.Log.Errorf("Error getting pod %s/%s: %v", address.TargetRef.Namespace, address.TargetRef.Name, err)
					continue
				}

				// check pod's resource request
				gpuCount := getGPUCount(pod)
				if gpuCount == 2 || gpuCount == 4 {
					for _, port := range subset.Ports {
						endpointsList = append(endpointsList, fmt.Sprintf("%s-%s:%d", serviceName, address.IP, port.Port))
					}
				}
			}
		}
	}
	return endpointsList
}

// get pod object
func getPod(namespace, name string) (*v1.Pod, error) {
	// assume we have a clientset
	return clientset.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}

// get gpu count from pod
func getGPUCount(pod *v1.Pod) int {
	gpuCount := 0
	for _, container := range pod.Spec.Containers {
		if val, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			gpuCount += int(val.Value())
		}
	}
	return gpuCount
}
*/
