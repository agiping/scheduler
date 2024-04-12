package scheduler

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

// SkyServeLoadBalancer struct definition
type SkyServeLoadBalancer struct {
	controllerURL       string
	loadBalancerPort    int
	loadBalancingPolicy LoadBalancingPolicy
	requestAggregator   *RequestAggregator
	server              *gin.Engine
	controllerSession   string
}

// NewSkyServeLoadBalancer creates a new SkyServeLoadBalancer
func NewSkyServeLoadBalancer(controllerURL string, loadBalancerPort int, policy LoadBalancingPolicy) *SkyServeLoadBalancer {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	lb := &SkyServeLoadBalancer{
		controllerURL:       controllerURL,
		loadBalancerPort:    loadBalancerPort,
		loadBalancingPolicy: policy,
		requestAggregator:   NewRequestAggregator(),
		server:              router,
	}

	router.Any("/*path", lb.handleRequest)
	return lb
}

// Run starts the load balancer
func (lb *SkyServeLoadBalancer) Run() {
	go lb.syncWithController()
	log.Printf("SkyServe Load Balancer started on http://0.0.0.0:%d\n", lb.loadBalancerPort)
	log.Fatal(lb.server.Run(fmt.Sprintf(":%d", lb.loadBalancerPort)))
}

// syncWithController handles syncing with the controller at regular intervals
func (lb *SkyServeLoadBalancer) syncWithController() {
	client := resty.New()
	for {
		time.Sleep(5 * time.Second) // Initial delay to allow the controller to bootstrap

		response, err := client.R().
			SetResult(map[string]interface{}{}).
			SetBody(map[string]interface{}{
				"request_aggregator": lb.requestAggregator.ToMap(),
				"controller_session": lb.controllerSession,
			}).
			Post(lb.controllerURL + "/controller/load_balancer_sync")

		if err != nil {
			log.Printf("Error syncing with controller: %v", err)
			continue
		}

		data := response.Result().(*map[string]interface{})
		lb.controllerSession = (*data)["controller_session"].(string)
		replicas := (*data)["ready_replica_urls"].([]interface{})

		urls := make([]string, len(replicas))
		for i, url := range replicas {
			urls[i] = url.(string)
		}

		lb.loadBalancingPolicy.SetReadyReplicas(urls)
	}
}

// handleRequest proxies incoming requests to the appropriate service replica
func (lb *SkyServeLoadBalancer) handleRequest(c *gin.Context) {
	path := c.Request.URL.Path
	fullPath := c.FullPath()

	requestCopy := copyRequest(c.Request)
	selectedReplica := lb.loadBalancingPolicy.SelectReplica(requestCopy)

	if selectedReplica == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "No ready replicas available"})
		return
	}

	proxyURL := fmt.Sprintf("%s%s", *selectedReplica, path)
	resp, err := proxyRequest(c.Request, proxyURL)

	if err != nil {
		log.Printf("Error proxying request to %s: %v", proxyURL, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to proxy request"})
		return
	}

	c.DataFromReader(resp.StatusCode, resp.ContentLength, resp.Header.Get("Content-Type"), resp.Body, nil)
	lb.loadBalancingPolicy.ReleaseConnection(*selectedReplica)
}

func proxyRequest(req *http.Request, url string) (*http.Response, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	return client.Do(req) // Simplified: copies the method, URL, body, headers
}

func copyRequest(original *http.Request) *http.Request {
	copy := new(http.Request)
	*copy = *original
	return copy
}

// Main function setup
func main() {
	controllerAddr := "http://localhost:8000"
	loadBalancerPort := 8080
	policy := NewLeastNumberOfRequestsPolicy() // Assuming this policy implementation is available

	lb := NewSkyServeLoadBalancer(controllerAddr, loadBalancerPort, policy)
	lb.Run()
}
