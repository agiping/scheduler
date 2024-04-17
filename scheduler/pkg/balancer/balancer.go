package balancer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"

	"scheduler/scheduler/pkg/policy"
	"scheduler/scheduler/pkg/utils"
)

// The time interval in seconds for load balancer to sync with controller. Every
// time the load balancer syncs with controller, it will update all available
// replica ips for each service, also send the number of requests in last query
// interval.
const LBControllerSyncIntervalSeconds = 20

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
	controllerSession string
	// the port where the load balancer listens to.
	loadBalancerPort int
	// configuration of load balancing policy
	loadBalancingPolicy *policy.LeastNumberOfRequestsPolicy
	requestAggregator   utils.RequestsAggregator
}

// Create a load balancer instance
func NewSkyServeLoadBalancer(controllerURL string, lbPort int) *SkyServeLoadBalancer {
	client := resty.New()
	client.SetTimeout(5 * time.Second)

	balancer := &SkyServeLoadBalancer{
		appServer:           gin.Default(),
		appClient:           client,
		controllerURL:       controllerURL,
		loadBalancerPort:    lbPort,
		loadBalancingPolicy: policy.NewLeastNumberOfRequestsPolicy(),
		requestAggregator:   utils.NewRequestTimestamp(),
	}

	// "/-/urls" is a special endpoint for deploying our scheduler,
	// which has higher priority than "/*path" during router matching.
	balancer.appServer.GET("/-/urls", balancer.getURLs)
	balancer.appServer.Any("/*path", balancer.handleRequest)

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

		var result map[string]interface{}
		if err := json.Unmarshal(resp.Body(), &result); err != nil {
			log.Printf("Failed to decode response from controller: %v\n", err)
			continue
		}

		readyReplicaUrls, _ := result["ready_replica_urls"].([]string)
		controllerSession, _ := result["controller_session"].(string)

		log.Printf("Controller session: %s\n", controllerSession)
		log.Printf("Ready replicas: %v\n", readyReplicaUrls)
		lb.controllerSession = controllerSession
		lb.loadBalancingPolicy.SetReadyReplicas(readyReplicaUrls)
		// Clean up after reporting request information to avoid OOM.
		lb.requestAggregator.Clear()

		time.Sleep(time.Duration(LBControllerSyncIntervalSeconds) * time.Second)

	}
}

func (lb *SkyServeLoadBalancer) getURLs(c *gin.Context) {
	readyReplicaUrls := lb.loadBalancingPolicy.ReadyReplicas
	for i, url := range readyReplicaUrls {
		if !startsWithHTTP(url) {
			readyReplicaUrls[i] = "http://" + url
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"controller": lb.controllerURL,
		"replicas":   readyReplicaUrls,
	})
}

// proxyRequest proxies the incoming request to the selected service replica.
func (lb *SkyServeLoadBalancer) proxyRequest(
	ctx context.Context,
	method, url string,
	bodyBytes []byte,
	headers http.Header,
	stream bool,
	callback func()) (*http.Response, error) {
	client := &http.Client{
		Timeout: time.Second * 370, // Setting overall timeout slightly more than read timeout
	}

	proxyReq, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	proxyReq.Header = headers

	if stream {
		resp, err := client.Do(proxyReq)
		if err != nil {
			return nil, err
		}
		go func() {
			defer resp.Body.Close()
			io.Copy(os.Stdout, resp.Body) // Example: stream to stdout, modify as needed.
			if callback != nil {
				callback()
			}
		}()
		return resp, nil
	} else {
		resp, err := client.Do(proxyReq)
		if callback != nil {
			callback()
		}
		return resp, err
	}
}

// handleRequest manages incoming requests by proxying them to service replicas.
func (lb *SkyServeLoadBalancer) handleRequest(c *gin.Context) {
	req := c.Request
	path := req.URL.Path
	var isStream bool
	var urlWithHTTP string
	// Omit the requests other than /generate_stream and /generate for autoscaling
	// e.g., heatlh check, metrics, etc.
	if strings.HasSuffix(path, "/generate_stream") || strings.HasSuffix(path, "/generate") {
		lb.requestAggregator.Add(req)
	}
	if strings.HasSuffix(path, "/generate_stream") {
		isStream = true
	}
	readyReplicaURL := lb.loadBalancingPolicy.SelectReplica(req) // Implement your load balancing policy to get replica URL

	if *readyReplicaURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "No ready replicas. Use 'sky serve status [SERVICE_NAME]' to check the replica status"})
		return
	}

	if !startsWithHTTP(*readyReplicaURL) {
		urlWithHTTP = "http://" + *readyReplicaURL
	} else {
		urlWithHTTP = *readyReplicaURL
	}
	targetURL := urlWithHTTP + path

	log.Printf("Proxying request to %s", targetURL)

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading request body"})
		return
	}

	resp, err := lb.proxyRequest(c,
		req.Method,
		targetURL,
		bodyBytes,
		req.Header,
		isStream,
		func() {
			lb.loadBalancingPolicy.UpdateNumberOfRequests(*readyReplicaURL)
		})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to proxy request: %v", err)})
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read proxied response"})
		return
	}

	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}

func startsWithHTTP(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func (lb *SkyServeLoadBalancer) Run() {
	go lb.syncWithController()

	log.Printf("Baichuan scheduler started on http://0.0.0.0:%d\n", lb.loadBalancerPort)
	lb.appServer.Run(fmt.Sprintf(":%d", lb.loadBalancerPort))
}
