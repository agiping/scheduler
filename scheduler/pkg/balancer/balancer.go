package balancer

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	appServer *gin.Engine
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
	loadBalancingPolicy    *policy.LeastNumberOfRequestsPolicy
	requestAggregator      utils.RequestsAggregator
	syncWithControllerChan chan bool
}

// Create a load balancer instance
func NewSkyServeLoadBalancer(controllerURL string, lbPort int) *SkyServeLoadBalancer {
	balancer := &SkyServeLoadBalancer{
		appServer:              gin.Default(),
		controllerURL:          controllerURL,
		loadBalancerPort:       lbPort,
		loadBalancingPolicy:    policy.NewLeastNumberOfRequestsPolicy(),
		requestAggregator:      utils.NewRequestTimestamp(),
		syncWithControllerChan: make(chan bool),
	}

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
		select {
		case <-lb.syncWithControllerChan:
			return
		default:
			client := resty.New()
			resp, err := client.R().
				SetHeader("Content-Type", "application/json").
				SetBody(map[string]interface{}{
					"request_aggregator": lb.requestAggregator.ToMap(),
					"controller_session": lb.controllerSession,
				}).
				Post(lb.controllerURL + "/controller/load_balancer_sync")

			if err != nil {
				log.Printf("An error occurred: %v\n", err)
				continue
			}

			var result map[string]interface{}
			if err := json.Unmarshal(resp.Body(), &result); err != nil {
				log.Printf("Failed to decode response from controller: %v\n", err)
				continue
			}

			readyReplicaUrls, _ := result["ready_replica_urls"].([]string)
			controllerSession, _ := result["controller_session"].(string)

			lb.controllerSession = controllerSession
			lb.loadBalancingPolicy.SetReadyReplicas(readyReplicaUrls)
			lb.requestAggregator.Clear()

			time.Sleep(time.Duration(LBControllerSyncIntervalSeconds) * time.Second)
		}
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

func (lb *SkyServeLoadBalancer) handleRequest(c *gin.Context) {
	request := c.Request
	path := request.URL.Path
	lb.requestAggregator.Add(request)

	readyReplicaUrl := lb.loadBalancingPolicy.SelectReplica(&policy.Request{})
	if readyReplicaUrl == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "No ready replicas."})
		return
	}

	targetUrl := *readyReplicaUrl + path
	c.Redirect(http.StatusTemporaryRedirect, targetUrl)
	lb.loadBalancingPolicy.UpdateNumberOfRequests(*readyReplicaUrl)
}

func startsWithHTTP(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func (lb *SkyServeLoadBalancer) Run() {
	go lb.syncWithController()

	log.Printf("Baichuan scheduler started on http://0.0.0.0:%d\n", lb.loadBalancerPort)
	lb.appServer.Run(fmt.Sprintf(":%d", lb.loadBalancerPort))
}
