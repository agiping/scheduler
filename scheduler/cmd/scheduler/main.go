package main

import (
	"flag"
	"io"
	"log"
	"os"

	"scheduler/scheduler/pkg/balancer"
	"scheduler/scheduler/pkg/config"
)

func initLog() {
	logFile, err := os.OpenFile("baichuan-scheduler.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal("Error opening log file:", err)
	}
	multiWriter := io.MultiWriter(os.Stderr, logFile)
	log.SetOutput(multiWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
}

func main() {
	schedulerPort := flag.Int("load-balancer-port", 8890, "Port on which the load balancer listens. Default: 8890.")
	loadBalancerPolicy := flag.String("policy", "least-number-of-requests", "Load balancing policy to use. Options: 'cache-aware', 'least-number-of-requests', 'round-robin'. Default: 'least-number-of-requests'.")
	flag.Parse()

	// Validate the load balancer port
	if *schedulerPort <= 0 || *schedulerPort > 65535 {
		log.Fatal("Load balancer port must be specified and in the valid range, e.g., [1, 65535]")
	}

	// Validate the configuration of load balancing policy
	// Set of valid load balancing policies
	validPolicies := map[string]bool{
		"cache-aware":              true,
		"least-number-of-requests": true,
		"round-robin":              true,
	}

	if !validPolicies[*loadBalancerPolicy] {
		log.Fatal("Invalid load balancing policy. Options: 'cache-aware', 'least-number-of-requests', 'round-robin'.")
	}

	initLog()
	schedulerConfig := &config.SchedulerConfig{
		LBPort:   *schedulerPort,
		LBPolicy: *loadBalancerPolicy,
	}

	lb := balancer.NewBaichuanScheduler(schedulerConfig)
	lb.Run()
}
