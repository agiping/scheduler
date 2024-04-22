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

	"scheduler/scheduler/pkg/metrics"
	"scheduler/scheduler/pkg/policy"
	"scheduler/scheduler/pkg/types"
	"scheduler/scheduler/pkg/utils"
)

const (
	// The time interval in seconds for load balancer to sync with controller. Every
	// time the load balancer syncs with controller, it will update all available
	// replica ips for each service, also send the number of requests in last query
	// interval.
	LBControllerSyncInterval = 10 * time.Second
	// Timeout for proxying requests to replicas.
	TimeOutOfRequestProxying = 600
)

// LoadBalancer structure for controlling proxying of endpoint replicas
type SkyServeLoadBalancer struct {
	// app server of the load balancer
	// TODO(Ping Zhang): Currently, we use gin, we will consider other high performance frameworks in case of need
	// e.g., Beego, Iris, Echo, Fiber, etc.
	appServer *gin.Engine
	// app client of the load balancer, used for sending requests to SkyServe controller
	appClient *resty.Client
	// controllerURL: the URL of the controller
	controllerURL string
	/***
	controllerSession is used to verify the controller to be the
	same one that the load balancer is connecting to,
	avoiding the case that the controller is restarted
	and the load balancer connects to the new
	controller immediately, causing the service to be
	unavailable, although the old replicas are still
	in service.
	***/
	controllerSession *string
	// the port where the load balancer listens to.
	loadBalancerPort int
	// TODO(Ping Zhang): We need to support configuration of load balancing policy
	loadBalancingPolicy *policy.CacheAwarePolicy
	requestAggregator   utils.RequestsAggregator
}

// Create a load balancer instance
func NewSkyServeLoadBalancer(controllerURL string, lbPort int) *SkyServeLoadBalancer {
	// Set the gin mode to release mode
	// uncomment to debug
	gin.SetMode(gin.ReleaseMode)
	client := resty.New()
	client.SetTimeout(5 * time.Second)

	balancer := &SkyServeLoadBalancer{
		appServer:           gin.Default(),
		appClient:           client,
		controllerURL:       controllerURL,
		loadBalancerPort:    lbPort,
		loadBalancingPolicy: policy.NewCacheAwarePolicy(),
		requestAggregator:   utils.NewRequestTimestamp(),
	}

	// "/-/urls" is a special endpoint for deploying scheduler.
	balancer.appServer.GET("/-/urls", balancer.getURLs)
	balancer.appServer.Any("/generate", balancer.handleRequest)
	balancer.appServer.Any("/generate_stream", balancer.handleRequest)

	return balancer
}

/*
**

Sync with controller periodically.

Every `constants.LB_CONTROLLER_SYNC_INTERVAL_SECONDS` seconds, the
load balancer will sync with the controller to get the latest
information about available replicas; also, it report the request
information to the controller, so that the controller can make
autoscaling decisions.

**
*/
func (lb *SkyServeLoadBalancer) syncWithController() {
	time.Sleep(5 * time.Second) // Wait for the controller to bootstrap

	for {
		resp, err := lb.appClient.R().
			SetHeader("Content-Type", "application/json").
			SetBody(map[string]interface{}{
				"request_aggregator": lb.requestAggregator.ToMap(),
				"controller_session": lb.controllerSession,
			}).
			Post(lb.controllerURL + "/controller/load_balancer_sync")

		if err != nil {
			log.Printf("Error occurred during sync with controller: %v\n", err)
			continue
		}

		log.Printf("Response from controller: %s\n", string(resp.Body()))
		var result map[string]interface{}
		if err := json.Unmarshal(resp.Body(), &result); err != nil {
			log.Printf("Failed to decode response from controller: %v\n", err)
			continue
		}

		var readyReplicaUrls []string
		// assert the type of ready_replica_urls
		if urls, ok := result["ready_replica_urls"].([]interface{}); ok {
			for _, url := range urls {
				if strUrl, ok := url.(string); ok {
					readyReplicaUrls = append(readyReplicaUrls, strUrl)
				}
			}
		} else {
			log.Printf("ready_replica_urls is expected to be []interface{}, while it is not.\n")
			continue
		}

		controllerSession, yes := result["controller_session"].(string)
		if !yes {
			log.Printf("controllerSession is expected to be a string, which is not.\n")
			continue
		}

		log.Printf("Controller session: %s\n", controllerSession)
		log.Printf("Ready replicas: %v\n", readyReplicaUrls)
		if lb.controllerSession == nil || *lb.controllerSession != controllerSession {
			lb.controllerSession = &controllerSession
		}

		lb.loadBalancingPolicy.SetReadyReplicas(readyReplicaUrls)
		// Clean up after reporting request information to avoid OOM.
		lb.requestAggregator.Clear()

		time.Sleep(LBControllerSyncInterval)

	}
}

func (lb *SkyServeLoadBalancer) getURLs(c *gin.Context) {
	lb.loadBalancingPolicy.PoLock.RLock()
	readyReplicas := lb.loadBalancingPolicy.ReadyReplicas
	lb.loadBalancingPolicy.PoLock.RUnlock()

	readyReplicaUrls := make([]string, len(readyReplicas))
	for i, pod := range readyReplicas {
		if !startsWithHTTP(pod.IP) {
			readyReplicaUrls[i] = "http://" + pod.IP
		} else {
			readyReplicaUrls[i] = pod.IP
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"controller": lb.controllerURL,
		"replicas":   readyReplicaUrls,
	})
}

// handleRequest manages incoming requests by proxying them to service replicas.
func (lb *SkyServeLoadBalancer) handleRequest(c *gin.Context) {
	req := c.Request
	path := req.URL.Path
	var isStream bool
	var urlWithHTTP string
	var requestBody types.RequestBody

	// Read the original body
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read request body: " + err.Error()})
		return
	}

	// Omit the requests other than /generate_stream and /generate for autoscaling
	// e.g., heatlh check, metrics, etc.
	if strings.HasSuffix(path, "/generate_stream") || strings.HasSuffix(path, "/generate") {
		lb.requestAggregator.Add(req)
	}
	if strings.HasSuffix(path, "/generate_stream") {
		isStream = true
	}

	req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	if err := json.NewDecoder(req.Body).Decode(&requestBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}
	readyReplicaURL := lb.loadBalancingPolicy.SelectReplica(&requestBody)

	if readyReplicaURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "No ready replicas. Use 'sky serve status [SERVICE_NAME]' to check the replica status"})
		return
	}

	if !startsWithHTTP(readyReplicaURL) {
		urlWithHTTP = "http://" + readyReplicaURL
	} else {
		urlWithHTTP = readyReplicaURL
	}
	targetURL := urlWithHTTP + path

	log.Printf("Proxying request to %s", targetURL)

	client := &http.Client{
		Timeout: time.Second * time.Duration(TimeOutOfRequestProxying),
	}
	proxyReq, err := http.NewRequestWithContext(c, req.Method, targetURL, io.NopCloser(bytes.NewBuffer(bodyBytes)))
	log.Printf("Created proxy request body: %v\n", proxyReq.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create proxy request: " + err.Error()})
		return
	}
	proxyReq.Header = req.Header.Clone()

	resp, err := client.Do(proxyReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to proxy request: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	setResponseHeaders(c, resp)

	if isStream {
		// Stream response directly to client
		io.Copy(c.Writer, resp.Body)
	} else {
		// For non-stream, read all and then send
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read proxied response"})
			return
		}
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	// Once finished, update the number of requests for the selected replica
	lb.loadBalancingPolicy.UpdateAfterResponse(readyReplicaURL)
}

func (lb *SkyServeLoadBalancer) startCollectingQueueSize() {
	client := &http.Client{}
	collector := metrics.NewTgiQueueSizeCollector(client)
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
		lb.loadBalancingPolicy.PoLock.RLock()
		ReadyReplicas := lb.loadBalancingPolicy.ReadyReplicas
		lb.loadBalancingPolicy.PoLock.RUnlock()
		for _, replica := range ReadyReplicas {
			wg.Add(1)
			go func(url string) {
				defer wg.Done()
				// obtain a permit
				concurrencyControl <- struct{}{}
				if err := collector.Collect(url); err != nil {
					fmt.Printf("Error collecting from %s: %v\n", url, err)
				} else {
					lb.loadBalancingPolicy.UpdateTgiQueueSize(&collector.ReplicaQueueSize)
				}
				// release the permit
				<-concurrencyControl
			}(replica.IP)
		}
		// wait for all requests to finish
		wg.Wait()
		roundEnd := time.Now()
		fmt.Printf("Round took %v\n", roundEnd.Sub(roundStart).Milliseconds())
		// print the queue size for each replica
		metrics.PrintSortedQueueSizes(collector)
	}
}

func (lb *SkyServeLoadBalancer) Run() {
	go lb.syncWithController()
	go lb.startCollectingQueueSize()

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
