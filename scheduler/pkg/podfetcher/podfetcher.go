package podfetcher

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func PodFetcher() []string {
	namespace := "inference-service"
	deploymentName := "chat-character-lite-online-sky"
	testContext := "context-cluster1"

	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: testContext})

	config, err := kubeconfig.ClientConfig()
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// Get the deployment to find the label selector
	deployment, err := clientset.AppsV1().Deployments(namespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	// Use the label selector from the Deployment to list pods
	labelSelector := labels.Set(deployment.Spec.Selector.MatchLabels).String()
	pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		panic(err.Error())
	}

	// Filter pods that are in Ready condition
	var readyPodIPs []string
	for _, pod := range pods.Items {
		if isPodReady(&pod) {
			readyPodIPs = append(readyPodIPs, pod.Status.PodIP)
		}
	}

	return readyPodIPs
}

// isPodReady checks if a pod is ready to serve requests.
func isPodReady(pod *v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}
