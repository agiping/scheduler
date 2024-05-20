package balancer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"

	"scheduler/scheduler/pkg/config"
	"scheduler/scheduler/pkg/logger"
	"scheduler/scheduler/pkg/types"
	"scheduler/scheduler/pkg/utils"
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
	scheduler := NewScheduler(true)
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
	go RunReplicas()
	logger.Init("debug")

	// testing retry policy enabled
	scheduler := NewScheduler(true)

	restyRequest := scheduler.appClient.
		R().
		EnableTrace().
		SetDoNotParseResponse(true).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"name": "ping", "inputs": "Write a song with code"})

	inferRequest := types.InferRequest{
		RequestID: "123",
	}
	restyRequest.SetContext(context.WithValue(restyRequest.Context(), "inferRequestID", inferRequest.RequestID))

	path := "/generate"

	pod := scheduler.loadBalancingPolicy.SelectReplica(&inferRequest)
	url := "http://" + pod + path
	resp, err := restyRequest.Execute("POST", url)
	if err != nil {
		t.Logf("Error executing request: %v", err)
	} else if resp.StatusCode() != http.StatusOK {
		t.Logf("Request failed with status: %d, response: %s", resp.StatusCode(), resp.String())
	}

	assert.Equal(t, 200, resp.StatusCode())

	finalPod := resp.Request.RawRequest.URL.Host
	assert.Equal(t, finalPod, "localhost:8892")

	scheduler.loadBalancingPolicy.UpdateAfterResponse(finalPod)
	nor := scheduler.loadBalancingPolicy.GetNumberOfRequests()
	assert.Equal(t, nor, map[string]int{"localhost:8891": 0, "localhost:8892": 0})

	// testing retry policy disabled
	scheduler = NewScheduler(false)
	assert.Equal(t, 0, scheduler.appClient.RetryCount)
	assert.Equal(t, time.Duration(100000000), scheduler.appClient.RetryWaitTime)
	assert.Equal(t, time.Duration(2000000000), scheduler.appClient.RetryMaxWaitTime)

	restyRequest =
		scheduler.
			appClient.
			R().
			EnableTrace().
			SetDoNotParseResponse(true).
			SetHeader("Content-Type", "application/json").
			SetBody(map[string]string{"name": "ping", "inputs": "Write a song with code"})

	pod = scheduler.loadBalancingPolicy.SelectReplica(&inferRequest)
	url = "http://" + pod + path
	resp, err = restyRequest.Execute("POST", url)
	finalPod = resp.Request.RawRequest.URL.Host
	assert.Equal(t, resp.StatusCode(), 500)
	assert.Equal(t, finalPod, "localhost:8891")

	scheduler.loadBalancingPolicy.UpdateAfterResponse(finalPod)
	nor = scheduler.loadBalancingPolicy.GetNumberOfRequests()
	assert.Equal(t, nor, map[string]int{"localhost:8891": 0, "localhost:8892": 0})
}

func TestConfigureRestyClient_Retry_No_Replicas_Retry(t *testing.T) {
	go RunReplicas()
	logger.Init("debug")

	scheduler := NewScheduler(true)
	scheduler.loadBalancingPolicy.SetReadyReplicas([]string{"localhost:8891"})

	restyRequest := scheduler.
		appClient.
		R().
		EnableTrace().
		SetDoNotParseResponse(true).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"name": "ping", "inputs": "Write a song with code"})

	inferRequest := types.InferRequest{
		RequestID: "123",
	}
	restyRequest.SetContext(context.WithValue(restyRequest.Context(), "inferRequestID", inferRequest.RequestID))

	pod := scheduler.loadBalancingPolicy.SelectReplica(&inferRequest)
	path := "/generate"
	url := "http://" + pod + path
	resp, err := restyRequest.Execute("POST", url)

	assert.NotNil(t, err)
	assert.Nil(t, resp)

	finalPod := restyRequest.URL
	assert.Equal(t, true, strings.Contains(finalPod, "localhost:8891"))
	nor := scheduler.loadBalancingPolicy.GetNumberOfRequests()
	assert.Equal(t, nor, map[string]int{"localhost:8891": 0})
}

func NewScheduler(enableRerty bool) *BaichuanScheduler {
	var replicas []string
	replicas = append(replicas, "localhost:8891")
	replicas = append(replicas, "localhost:8892")

	sconfig := &config.SchedulerConfig{
		LBPolicy: "least-number-of-requests",
		TimeoutPolicy: config.TimeoutPolicy{
			DefaultTimeout: 30 * time.Second,
		},
		RetryPolicy: config.RetryPolicy{
			EnableRetry:          enableRerty,
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

func RunReplicas() {
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
	router := gin.Default()
	router.POST("/generate", handleRequest1)
	router.POST("/generate_stream", handleRequest1)
	fmt.Println("Server 1 started on port 8891")
	router.Run(":8891")
}

// simulate a server that returns a successful response
func startServer2(wg *sync.WaitGroup) {
	defer wg.Done()

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.POST("/generate", handleRequest2)
	router.POST("/generate_stream", handleRequest2)
	fmt.Println("Server 2 started on port 8892")
	router.Run(":8892")
}

func handleRequest1(c *gin.Context) {
	fmt.Println("Received request on server 1")
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": "Service is not available",
	})
}

func handleRequest2(c *gin.Context) {
	fmt.Println("Received request on server 2")
	if c.Request.URL.Path == "/generate_stream" {
		c.Stream(func(w io.Writer) bool {
			_, _ = w.Write([]byte(buildLongStrings()))
			return false
		})
		return
	} else {
		bodyBytes, _ := io.ReadAll(c.Request.Body)
		fmt.Println("Request body:", string(bodyBytes))
		c.JSON(http.StatusOK, gin.H{
			"server": "server2",
		})
	}
}

func TestSyncReplicas(t *testing.T) {
	logger.Init("debug")
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
	scheduler := NewBaichuanScheduler(sconfig)
	replicas := []string{"localhost:8891", "localhost:8892"}
	go scheduler.syncReplicas()

	utils.ReadyEndpointsChan <- replicas
	time.Sleep(1 * time.Second)
	assert.Equal(t, scheduler.loadBalancingPolicy.GetStringReadyReplicas(), replicas)
}

func TestHandleRequest(t *testing.T) {
	logger.Init("info")

	// simulate tgi instances
	// 8891, 8892
	go RunReplicas()

	// run the scheduler: 8890
	scheduler := NewScheduler(true)
	go scheduler.appServer.Run(":8890")

	// request to the scheduler
	client := resty.New()
	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"inputs": "Write a song with code", "name": "ping"}).
		Post("http://localhost:8890/generate")

	// basic validation
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, 1, resp.Request.Attempt)
	assert.Equal(t, "localhost:8890", resp.Request.RawRequest.URL.Host)

	// ensure all connections are released
	nor := scheduler.loadBalancingPolicy.GetNumberOfRequests()
	assert.Equal(t, nor, map[string]int{"localhost:8891": 0, "localhost:8892": 0})

	// Invalid request
	resp, err = client.R().
		SetHeader("Content-Type", "application/json").
		SetBody("inputs Write a song with code name ping").
		Post("http://localhost:8890/generate")

	assert.Nil(t, err)
	assert.Equal(t, 1, resp.Request.Attempt)
	assert.Equal(t, 400, resp.StatusCode())

	// generate stream
	// remove 8891 from the list of ready replicas
	scheduler.loadBalancingPolicy.SetReadyReplicas([]string{"localhost:8892"})
	resp, err = client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"inputs": "Write a song with code", "name": "ping"}).
		Post("http://localhost:8890/generate_stream")

	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, buildLongStrings(), string(resp.Body()))
	nor = scheduler.loadBalancingPolicy.GetNumberOfRequests()
	assert.Equal(t, nor, map[string]int{"localhost:8892": 0})
}

func buildLongStrings() string {
	str := "This is awesome. Coding is a creative challenge and a journey of endless learning." +
		" It is a way to express your thoughts and ideas in a way that machines can understand." +
		" It is a way to solve problems and create solutions that can make a difference in the world."
	return str
}

func TestLogAndRespondError(t *testing.T) {
	logger.Init("info")
	// Set up the gin.Context
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Call with an error
	err := errors.New("test error")
	logAndRespondError(c, http.StatusInternalServerError, "An error occurred", err)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, `{"error":"An error occurred: test error"}`, w.Body.String())

	// Call without an error
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	logAndRespondError(c, http.StatusServiceUnavailable, "An error occurred", nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, `{"error":"An error occurred"}`, w.Body.String())
}

func TestFormatURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		path     string
		expected string
	}{
		{
			name:     "baseURL with http prefix",
			baseURL:  "http://example.com",
			path:     "/test",
			expected: "http://example.com/test",
		},
		{
			name:     "baseURL without http prefix",
			baseURL:  "example.com",
			path:     "/test",
			expected: "http://example.com/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := formatURL(tt.baseURL, tt.path)
			if actual != tt.expected {
				t.Errorf("formatURL(%q, %q) = %q; want %q", tt.baseURL, tt.path, actual, tt.expected)
			}
		})
	}
}

func TestSetResponseHeaders(t *testing.T) {
	logger.Init("info")
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		resp     *http.Response
		expected http.Header
	}{
		{
			name: "Response is not nil",
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
			},
			expected: http.Header{
				"Content-Type": []string{"application/json"},
			},
		},
		{
			name:     "Response is nil",
			resp:     nil,
			expected: http.Header{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			lb := &BaichuanScheduler{}
			lb.setResponseHeaders(c, tt.resp)

			assert.Equal(t, map[string][]string(tt.expected), map[string][]string(c.Writer.Header()))
		})
	}
}
