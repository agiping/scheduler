package balancer

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"

	"scheduler/scheduler/pkg/config"
	"scheduler/scheduler/pkg/policy"
)

func TestNewBaichuanScheduler(t *testing.T) {
	sconfig := &config.SchedulerConfig{
		LBPort:   8080,
		LBPolicy: "round-robin",
	}

	scheduler := NewBaichuanScheduler(sconfig)

	routes := scheduler.appServer.Routes()

	for _, route := range routes {
		t.Logf("Method: %s, Path: %s", route.Method, route.Path)
	}

	expectedRoutes := []struct {
		Method string
		Path   string
	}{
		{"GET", "/generate"},
		{"POST", "/generate"},
		{"PUT", "/generate"},
		{"DELETE", "/generate"},
		{"PATCH", "/generate"},
		{"HEAD", "/generate"},
		{"OPTIONS", "/generate"},
		{"CONNECT", "/generate"},
		{"TRACE", "/generate"},
		{"GET", "/generate_stream"},
		{"POST", "/generate_stream"},
		{"PUT", "/generate_stream"},
		{"DELETE", "/generate_stream"},
		{"PATCH", "/generate_stream"},
		{"HEAD", "/generate_stream"},
		{"OPTIONS", "/generate_stream"},
		{"CONNECT", "/generate_stream"},
		{"TRACE", "/generate_stream"},
	}

	for _, expectedRoute := range expectedRoutes {
		found := false
		for _, route := range routes {
			if route.Method == expectedRoute.Method && route.Path == expectedRoute.Path {
				found = true
				break
			}
		}
		assert.True(t, found, "Route not found: Method=%s, Path=%s", expectedRoute.Method, expectedRoute.Path)
	}
}

func TestConfigureRestyClient(t *testing.T) {
	LBPolicy := policy.NewLeastNumberOfRequestsPolicy()
	sconfig := &config.SchedulerConfig{
		TimeoutPolicy: config.TimeoutPolicy{
			DefaultTimeout: 30 * time.Second,
		},
		RetryPolicy: config.RetryPolicy{
			EnableRetry:          true,
			MaxRetryTimes:        3,
			DefaultRetryDelay:    1 * time.Second,
			MaxRetryDelay:        5 * time.Second,
			RetriableStatusCodes: []int{500, 502},
		},
	}

	client := configureRestyClient(LBPolicy, sconfig)

	// Assert timeout
	assert.Equal(t, 30*time.Second, client.GetClient().Timeout)

	// Assert transport settings
	transport := client.GetClient().Transport.(*http.Transport)
	assert.Equal(t, MaxIdleConnsInClientPool, transport.MaxIdleConns)
	assert.Equal(t, 90*time.Second, transport.IdleConnTimeout)
	assert.True(t, transport.DisableCompression)

	// Assert retry policy
	assert.Equal(t, 3, client.RetryCount)
	assert.Equal(t, 1*time.Second, client.RetryWaitTime)
	assert.Equal(t, 5*time.Second, client.RetryMaxWaitTime)

	retryCondition := client.RetryConditions[0]

	// Simulate a retry status code
	responseNeedRetry := &resty.Response{
		RawResponse: &http.Response{StatusCode: 500},
	}
	assert.True(t, retryCondition(responseNeedRetry, nil))

	// Simulate a non-retry status code
	responseNoRetry := &resty.Response{
		RawResponse: &http.Response{StatusCode: 200},
	}
	assert.False(t, retryCondition(responseNoRetry, nil))

	// Similate a retry error
	err := errors.New("retry error")
	assert.True(t, retryCondition(nil, err))
}
