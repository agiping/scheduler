package main

import (
	"flag"
	"log"

	"scheduler/scheduler/pkg/balancer"
)

func main() {
	controllerAddr := flag.String("controller-addr", "127.0.0.1", "The address of the controller.")
	loadBalancerPort := flag.Int("load-balancer-port", 8890, "The port where the load balancer listens to.")
	flag.Parse()

	if *controllerAddr == "" || *loadBalancerPort == 0 {
		log.Fatal("Controller address and load balancer port must be specified")
	}

	// Assuming the function NewSkyServeLoadBalancer and its related LoadBalancingPolicy are implemented
	// policy := NewLeastNumberOfRequestsPolicy() // Assuming this policy implementation is available
	lb := balancer.NewSkyServeLoadBalancer(*controllerAddr, *loadBalancerPort)
	lb.Run()
}
