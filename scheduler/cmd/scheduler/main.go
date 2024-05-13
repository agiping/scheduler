package main

import (
	"flag"
	"time"

	"scheduler/scheduler/pkg/balancer"
	"scheduler/scheduler/pkg/config"
	"scheduler/scheduler/pkg/logger"
	"scheduler/scheduler/pkg/utils"
)

func main() {
	// log level
	logLevel := flag.String("log-level", "info", "Log level for the logger. Options: 'info', 'debug', 'warn', 'error'. Default: 'info'.")

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
	defaultTimeout := flag.Int("default-timeout-seconds", 600, "Default timeout for requests")
	connectTimeout := flag.Int("connect-timeout-seconds", 10, "Timeout for establishing connections")

	flag.Parse()

	// Initialize the logger
	logger.Init(*logLevel)

	// Validate the load balancer port
	if *schedulerPort <= 0 || *schedulerPort > 65535 {
		logger.Log.Error("Load balancer port must be specified in the valid range, e.g., [1, 65535]")
		// fallback to default port when mis-configured
		logger.Log.Warn("Falling back to default port 8890")
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
		logger.Log.Error("Invalid load balancing policy. Options: 'cache-aware', 'least-number-of-requests', 'round-robin'.")
		// fallback to default policy when mis-confgured
		logger.Log.Warn("Falling back to default policy 'least-number-of-requests'")
		*loadBalancerPolicy = "least-number-of-requests"
	}

	// Parse the retry status code
	codes := []int{}
	codes, err := utils.ParseRetryStatusCodes(*retriableStatusCodes)
	if err != nil || len(codes) == 0 {
		logger.Log.Errorf("Error parsing retriable status codes: %v", err)
		logger.Log.Warnf("Falling back to default retriable status codes: %v", config.DefaultRetryCodes)
		codes = config.DefaultRetryCodes
	}
	if *maxRetryTimes < 0 || *maxRetryTimes > 10 {
		logger.Log.Error("Max retry times must be in the range [0, 10]")
		// fallback to default max retry times when mis-configured
		logger.Log.Warn("Falling back to default max retry times: 3")
		*maxRetryTimes = 3
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
