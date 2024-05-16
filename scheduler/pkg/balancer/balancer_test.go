package balancer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"

	"scheduler/scheduler/pkg/config"
	"scheduler/scheduler/pkg/logger"
	"scheduler/scheduler/pkg/types"
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
	scheduler := NewScheduler()
	// Assert timeout
	assert.Equal(t, 30*time.Second, scheduler.appClient.GetClient().Timeout)

	// Assert transport settings
	transport := scheduler.appClient.GetClient().Transport.(*http.Transport)
	assert.Equal(t, MaxIdleConnsInClientPool, transport.MaxIdleConns)
	assert.Equal(t, 90*time.Second, transport.IdleConnTimeout)
	assert.True(t, transport.DisableCompression)

	// Assert retry policy
	assert.Equal(t, 3, scheduler.appClient.RetryCount)
	assert.Equal(t, 1*time.Second, scheduler.appClient.RetryWaitTime)
	assert.Equal(t, 5*time.Second, scheduler.appClient.RetryMaxWaitTime)

	retryCondition := scheduler.appClient.RetryConditions[0]

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

func TestConfigureRestyClient_Retry(t *testing.T) {
	go MockServers()
	logger.Init("debug")
	scheduler := NewScheduler()

	restyRequest := scheduler.appClient.
		R().
		EnableTrace().
		SetDoNotParseResponse(false).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"name": "ping", "inputs": "Write a song with code"})

	inferRequest := types.InferRequest{
		RequestID: "123",
	}

	restyRequest.SetContext(context.WithValue(restyRequest.Context(), "inferRequestID", inferRequest.RequestID))

	pod := scheduler.loadBalancingPolicy.SelectReplica(&inferRequest)
	path := "/generate"
	url := pod + path
	url = "http://" + url
	resp, err := restyRequest.Execute("POST", url)
	if err != nil {
		t.Fatalf("Error executing request: %v", err)
	}

	body := resp.Body()
	t.Logf("Statuscode: %d", resp.StatusCode())
	t.Logf("Response body: %s", string(body))

	scheduler.loadBalancingPolicy.PrintNumberOfRequests()
}

func NewScheduler() *BaichuanScheduler {
	var replicas []string
	replicas = append(replicas, "localhost:8891")
	replicas = append(replicas, "localhost:8892")

	sconfig := &config.SchedulerConfig{
		LBPolicy: "least-number-of-requests",
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

	sched := NewBaichuanScheduler(sconfig)
	sched.loadBalancingPolicy.SetReadyReplicas(replicas)

	return sched
}

func MockServers() {
	var wg sync.WaitGroup
	wg.Add(2)

	go startServer1(&wg)
	go startServer2(&wg)

	wg.Wait()
}

// simulate a server that returns an error
func startServer1(wg *sync.WaitGroup) {
	defer wg.Done()

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.POST("/generate", handleRequest1)
	fmt.Println("Server 1 started on port 8891")
	router.Run(":8891")
}

// simulate a server that returns a successful response
func startServer2(wg *sync.WaitGroup) {
	defer wg.Done()

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.POST("/generate", handleRequest2)
	fmt.Println("Server 2 started on port 8892")
	router.Run(":8892")
}

func handleRequest1(c *gin.Context) {
	fmt.Println("Received request on server 1")
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": "request failed",
	})
}

func handleRequest2(c *gin.Context) {
	fmt.Println("Received request on server 2")
	bodyBytes, _ := io.ReadAll(c.Request.Body)
	fmt.Println("Request body:", string(bodyBytes))

	c.JSON(http.StatusOK, gin.H{
		"server": "server2",
		"path":   "generate",
	})
}
