package balancer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"

	"scheduler/scheduler/pkg/config"
	"scheduler/scheduler/pkg/endpointwatcher"
	"scheduler/scheduler/pkg/metrics"
	"scheduler/scheduler/pkg/policy"
	"scheduler/scheduler/pkg/types"
	"scheduler/scheduler/pkg/utils"
)

const (
	// The time interval in Millisecond for scheduler to sync replica info.
	SyncReplicaInterval = 100 * time.Millisecond
	// Timeout for proxying requests to replicas.
	TimeOutOfRequestProxying = 600
	// The maximum number of idle connections in the client pool.
	MaxIdleConnsInClientPool = 100
)

// LoadBalancer structure for controlling proxying of endpoint replicas
type BaichuanScheduler struct {
	// app server of the load balancer
	// TODO(Ping Zhang): Currently, we use gin, we will consider other high performance frameworks in case of need
	// e.g., Beego, Iris, Echo, Fiber, etc.
	appServer *gin.Engine
	appClient *resty.Client
	// the port where the load balancer listens to.
	loadBalancerPort int
	// TODO(Ping Zhang): We need to support configuration of load balancing policy
	// and metric aggregation strategy.
	loadBalancingPolicy policy.LoadBalancingPolicy
	schedulerConfig     *config.SchedulerConfig
}

// Create a scheduler instance
func NewBaichuanScheduler(sconfig *config.SchedulerConfig) *BaichuanScheduler {
	// Set the gin mode to release mode
	// uncomment to debug
	gin.SetMode(gin.ReleaseMode)

	// Create and configure resty client
	client := resty.New()
	client.SetTimeout(sconfig.TimeoutPolicy.DefaultTimeout)
	client.SetContentLength(true)
	client.SetTransport(&http.Transport{
		MaxIdleConns:       MaxIdleConnsInClientPool,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: true,
	})

	balancer := &BaichuanScheduler{
		appServer:        gin.Default(),
		appClient:        client,
		loadBalancerPort: sconfig.LBPort,
	}

	balancer.schedulerConfig = sconfig

	// Initialize the load balancing policy
	switch sconfig.LBPolicy {
	case "least-number-of-requests":
		balancer.loadBalancingPolicy = policy.NewLeastNumberOfRequestsPolicy()
	case "round-robin":
		balancer.loadBalancingPolicy = policy.NewRoundRobinPolicy()
	case "cache-aware":
		balancer.loadBalancingPolicy = policy.NewCacheAwarePolicy()
	default:
		log.Fatalf("Invalid load balancing policy: %s", sconfig.LBPolicy)
	}

	balancer.appServer.Any("/generate", balancer.handleRequest)
	balancer.appServer.Any("/generate_stream", balancer.handleRequest)

	return balancer
}

func (lb *BaichuanScheduler) syncReplicas() {
	ticker := time.NewTicker(SyncReplicaInterval)
	defer ticker.Stop()

	for {
		select {
		case ReadyEndpoints := <-utils.ReadyEndpointsChan:
			log.Printf("Ready replicas updated: %v\n", ReadyEndpoints)
			lb.loadBalancingPolicy.SetReadyReplicas(ReadyEndpoints)
		case <-ticker.C:
			// just to keep the loop running
		}
	}
}

// handleRequest manages incoming requests by proxying them to service replicas.
func (lb *BaichuanScheduler) handleRequest(c *gin.Context) {
	inferRequest := types.InferRequest{
		RequestID: uuid.New().String(),
	}

	// Read the original body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read request body: " + err.Error()})
		return
	}

	path := c.Request.URL.Path
	isStream := strings.HasSuffix(path, "/generate_stream")

	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	if err := json.NewDecoder(c.Request.Body).Decode(&inferRequest.Body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	readyReplicaURL := lb.loadBalancingPolicy.SelectReplica(&inferRequest)

	if readyReplicaURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "No ready replicas."})
		return
	}

	var urlWithHTTP string
	if !startsWithHTTP(readyReplicaURL) {
		urlWithHTTP = "http://" + readyReplicaURL
	} else {
		urlWithHTTP = readyReplicaURL
	}

	targetURL := urlWithHTTP + path

	//SetDoNotParseResponse(true).
	restyRequest := lb.appClient.R().
		SetBody(bodyBytes).
		SetContext(c)

	for key, values := range c.Request.Header {
		for _, value := range values {
			restyRequest.SetHeader(key, value)
		}
	}

	log.Printf("Proxying request to %s", targetURL)
	resp, err := restyRequest.Execute(c.Request.Method, targetURL)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to proxy request: " + err.Error()})
		return
	}

	setResponseHeaders(c, resp.RawResponse)
	//defer resp.RawResponse.Body.Close()

	if isStream {
		// Stream response directly to client
		_, err := io.Copy(c.Writer, resp.RawResponse.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to stream proxied response"})
			return
		}
	} else {
		// For non-stream, read all and then send
		body, err := io.ReadAll(resp.RawResponse.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read proxied response"})
			return
		}
		c.Data(resp.StatusCode(), resp.Header().Get("Content-Type"), body)
	}
	// Once finished, update the number of requests for the selected replica
	lb.loadBalancingPolicy.UpdateAfterResponse(readyReplicaURL)
}

func (lb *BaichuanScheduler) StartCollectingQueueSize() {
	client := &http.Client{}
	collector := metrics.NewTgiMetricCollector(client)
	ticker := time.NewTicker(metrics.CollectionInterval)
	defer ticker.Stop()

	// Wait for the loadbalancer to sync with the controller for the first time
	time.Sleep(10 * time.Second)
	// Limit the number of concurrent requests
	concurrencyControl := make(chan struct{}, metrics.MaxConcurrency)

	for {
		<-ticker.C
		roundStart := time.Now()
		var wg sync.WaitGroup
		// TODO (Ping Zhang): refactor to use a channel to update the ReadyReplicas only when its changed,
		// by that way, we can reduce the usage of lock and improve the performance.
		lb.loadBalancingPolicy.GetLock().Lock()
		ReadyReplicas := lb.loadBalancingPolicy.GetReadyReplicas()
		lb.loadBalancingPolicy.GetLock().Unlock()
		for _, replica := range ReadyReplicas {
			wg.Add(1)
			go func(url string) {
				defer wg.Done()
				// obtain a permit
				concurrencyControl <- struct{}{}
				if err := collector.Collect(url); err != nil {
					log.Printf("Error collecting from %s: %v\n", url, err)
				} else {
					lb.loadBalancingPolicy.UpdateTgiQueueSize(&collector.ReplicaMetrics)
				}
				// release the permit
				<-concurrencyControl
			}(replica.IP)
		}
		// wait for all requests to finish
		wg.Wait()
		roundEnd := time.Now()
		log.Printf("Round took %v\n", roundEnd.Sub(roundStart).Milliseconds())
		// print the queue size for each replica
		metrics.PrintSortedTgiMetric(collector)
	}
}

func (lb *BaichuanScheduler) Run() {
	go endpointwatcher.WatchEndpoints(lb.schedulerConfig)
	go lb.syncReplicas()

	log.Printf("Baichuan scheduler started on http://0.0.0.0:%d\n", lb.loadBalancerPort)
	lb.appServer.Run(fmt.Sprintf(":%d", lb.loadBalancerPort))
}

func setResponseHeaders(c *gin.Context, resp *http.Response) {
	c.Writer.WriteHeader(resp.StatusCode)
	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
}

func startsWithHTTP(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
