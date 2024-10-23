package config

import "time"

var DefaultRetryCodes = []int{500, 502, 503, 504}

// A placeholder for scheduler configuration.
type SchedulerConfig struct {
	LBPort                            int
	LBPolicy                          string
	Namespace                         string
	ServiceNames                      []string
	NumOfServices                     int
	RetryPolicy                       RetryPolicy
	TimeoutPolicy                     TimeoutPolicy
	RequestLengthDispatchingThreshold int
}

type RetryPolicy struct {
	EnableRetry          bool
	RetriableStatusCodes []int
	MaxRetryTimes        int
	DefaultRetryDelay    time.Duration
	MaxRetryDelay        time.Duration
	BackoffStrategy      string
}

type TimeoutPolicy struct {
	EnableTimeout  bool
	DefaultTimeout time.Duration
	ConnectTimeout time.Duration
}
