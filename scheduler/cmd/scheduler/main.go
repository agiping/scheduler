package main

import (
	"flag"
	"log"

	"scheduler/scheduler/pkg/balancer"
)

func main() {
	controllerAddr := flag.String("controller-addr", "http://127.0.0.1", "The address of the controller.")
	loadBalancerPort := flag.Int("load-balancer-port", 8890, "The port where the load balancer listens to.")
	flag.Parse()

	if *controllerAddr == "" {
		log.Fatal("Controller address must be specified")
	}

	if *loadBalancerPort <= 0 || *loadBalancerPort > 65535 {
		log.Fatal("Load balancer port must be specified and in the valid range, e.g., [1, 65535]")
	}

	lb := balancer.NewSkyServeLoadBalancer(*controllerAddr, *loadBalancerPort)
	lb.Run()
}
