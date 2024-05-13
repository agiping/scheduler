package main

import (
	"flag"
	"io"
	"log"
	"os"
	"time"

	"scheduler/scheduler/pkg/balancer"
	"scheduler/scheduler/pkg/config"
	"scheduler/scheduler/pkg/utils"
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
	// Initialize the logger
	initLog()

	// namespace, serviceName
	namespace := flag.String("namespace", "inference-service", "Namespace of the Service-Scheduler. Default: 'inference-service'.")
	serviceName := flag.String("service-name", "chat-character-lite-online-sky", "Name of the Service. Default: 'chat-character-lite-online-sky'.")

	// schedulerPort, loadBalancerPolicy
	schedulerPort := flag.Int("load-balancer-port", 8890, "Port on which the load balancer listens. Default: 8890.")
	loadBalancerPolicy := flag.String("policy", "least-number-of-requests", "Load balancing policy to use. Options: 'cache-aware', 'least-number-of-requests', 'round-robin'. Default: 'least-number-of-requests'.")

	// retry policy
	enableRetry := flag.Bool("enable-retry", true, "Enable or disable retry mechanism")
	retriableStatusCodes := flag.String("retriable-status-codes", "500,502,503,504", "HTTP status codes that are retriable")
	maxRetryTimes := flag.Int("max-retry-times", 3, "Maximum number of retries")
	defaultRetryDelay := flag.Int("default-retry-delay-seconds", 1, "Default delay between retries")
	maxRetryDelay := flag.Int("max-retry-delay-seconds", 20, "Maximum delay between retries")
	backoffStrategy := flag.String("backoff-strategy", "exponential", "Backoff strategy to use (fixed, linear, exponential)")

	// timeout policy
	enableTimeout := flag.Bool("enable-timeout", true, "Enable or disable timeouts")
	defaultTimeout := flag.Int("default-timeout-seconds", 600, "Default timeout for requests")
	connectTimeout := flag.Int("connect-timeout-seconds", 10, "Timeout for establishing connections")

	flag.Parse()

	// Validate the load balancer port
	if *schedulerPort <= 0 || *schedulerPort > 65535 {
		log.Println("Load balancer port must be specified and in the valid range, e.g., [1, 65535]")
		// fallback to default port when mis-configured
		*schedulerPort = 8890
	}

	// Validate the configuration of load balancing policy
	// Set of valid load balancing policies
	validPolicies := map[string]bool{
		"cache-aware":              true,
		"least-number-of-requests": true,
		"round-robin":              true,
	}

	if !validPolicies[*loadBalancerPolicy] {
		log.Println("Invalid load balancing policy. Options: 'cache-aware', 'least-number-of-requests', 'round-robin'.")
		// fallback to default policy when mis-confgured
		*loadBalancerPolicy = "least-number-of-requests"
	}

	// Parse the retry status code
	codes := []int{}
	codes, err := utils.ParseRetryStatusCodes(*retriableStatusCodes)
	if err != nil {
		log.Fatalf("Error parsing retriable status codes: %v", err)
	}
	if len(codes) == 0 {
		log.Println("No valid status codes provided for retry, use defaults")
		codes = config.DefaultRetryCodes
	}

	if *maxRetryTimes < 0 || *maxRetryTimes > 10 {
		log.Println("Max retry times must be in the range [0, 10]")
	}

	retryPolicy := &config.RetryPolicy{
		EnableRetry:          *enableRetry,
		MaxRetryTimes:        *maxRetryTimes,
		BackoffStrategy:      *backoffStrategy,
		RetriableStatusCodes: codes,
		DefaultRetryDelay:    time.Duration(*defaultRetryDelay) * time.Second,
		MaxRetryDelay:        time.Duration(*maxRetryDelay) * time.Second,
	}

	timeoutPolicy := &config.TimeoutPolicy{
		EnableTimeout:  *enableTimeout,
		DefaultTimeout: time.Duration(*defaultTimeout) * time.Second,
		ConnectTimeout: time.Duration(*connectTimeout) * time.Second,
	}

	schedulerConfig := &config.SchedulerConfig{
		LBPort:        *schedulerPort,
		LBPolicy:      *loadBalancerPolicy,
		Namespace:     *namespace,
		ServiceName:   *serviceName,
		RetryPolicy:   *retryPolicy,
		TimeoutPolicy: *timeoutPolicy,
	}

	lb := balancer.NewBaichuanScheduler(schedulerConfig)
	lb.Run()
}
