package main

import (
	"flag"
	"fmt"
	"log"

	"scheduler/scheduler/pkg/balancer"
	"scheduler/scheduler/pkg/config"
)

func main() {
	controllerAddr := flag.String("controller-addr", "http://127.0.0.1", "The address of the controller.")
	loadBalancerPort := flag.Int("load-balancer-port", 8890, "The port where the load balancer listens to.")

	// valid policies: "least-number-of-requests", "round-robin", "cache-aware"
	// default policy: "least-number-of-requests"
	// TODO(Ping Zhang): validate the lb policy configuration
	loadBalancerPolicy := flag.String("policy", "least-number-of-requests", "The load balancer policy to use.")
	fmt.Println(loadBalancerPolicy)
	flag.Parse()

	if *controllerAddr == "" {
		log.Fatal("Controller address must be specified")
	}

	if *loadBalancerPort <= 0 || *loadBalancerPort > 65535 {
		log.Fatal("Load balancer port must be specified and in the valid range, e.g., [1, 65535]")
	}

	schedulerConfig := &config.SchedulerConfig{
		ControllerURL: *controllerAddr,
		LBPort:        *loadBalancerPort,
		LBPolicy:      *loadBalancerPolicy,
	}

	lb := balancer.NewSkyServeLoadBalancer(schedulerConfig)
	lb.Run()
}
