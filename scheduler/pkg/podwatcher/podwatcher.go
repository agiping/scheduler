package podwatcher

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"scheduler/scheduler/pkg/utils"
)

func WatchPods() {
	namespace := "inference-service"
	deploymentName := "chat-character-lite-online-sky"

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	deployment, err := clientset.AppsV1().Deployments(namespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}
	labelSelector := labels.Set(deployment.Spec.Selector.MatchLabels).String()

	watcher, err := clientset.CoreV1().Pods(namespace).Watch(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		panic(err.Error())
	}

	var readyPodIPs []string
	for event := range watcher.ResultChan() {
		pod, ok := event.Object.(*v1.Pod)
		if !ok {
			continue
		}

		switch event.Type {
		case watch.Added, watch.Modified:
			if isPodReady(pod) {
				if !contains(readyPodIPs, pod.Status.PodIP) {
					readyPodIPs = append(readyPodIPs, pod.Status.PodIP)
				}
			} else {
				readyPodIPs = remove(readyPodIPs, pod.Status.PodIP)
			}
		case watch.Deleted:
			readyPodIPs = remove(readyPodIPs, pod.Status.PodIP)
		}
		utils.ReadyPodIPsChan <- readyPodIPs
	}
}

func isPodReady(pod *v1.Pod) bool {
	// if a pod is being deleted, it is not ready immediately
	// so we remove it from the list of ready pods, preventing
	// new requests from being sent to it.
	if pod.ObjectMeta.DeletionTimestamp != nil {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func remove(slice []string, item string) []string {
	for i, s := range slice {
		if s == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
