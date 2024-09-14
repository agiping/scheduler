package balancer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"

	"scheduler/scheduler/pkg/config"
	"scheduler/scheduler/pkg/endpointwatcher"
	"scheduler/scheduler/pkg/logger"
	"scheduler/scheduler/pkg/policy"
	"scheduler/scheduler/pkg/types"
	"scheduler/scheduler/pkg/utils"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// The time interval in Millisecond for scheduler to sync replica info.
	SyncReplicaInterval = 100 * time.Millisecond
	// Timeout for proxying requests to replicas.
	TimeOutOfRequestProxying = 600
	// The maximum number of idle connections in the client pool.
	MaxIdleConnsInClientPool = 200
)

// LoadBalancer structure for controlling proxying of endpoint replicas
type BaichuanScheduler struct {
	// app server of the load balancer
	// TODO(Ping Zhang): Currently, we use gin, we will consider other
	// high performance frameworks in case of need
	// e.g., Beego, Iris, Echo, Fiber, etc.
	appServer *gin.Engine
	appClient *resty.Client
	// the port where the load balancer listens to.
	loadBalancerPort int
	// TODO(Ping Zhang): We need to support configuration of load balancing policy
	// and metric aggregation strategy.
	loadBalancingPolicy policy.LoadBalancingPolicy
	schedulerConfig     *config.SchedulerConfig
	// request counts
	request_total prometheus.Counter
}

func configureRestyClient(lbpolicy policy.LoadBalancingPolicy, sconfig *config.SchedulerConfig) *resty.Client {
	client := resty.New()
	client.SetTimeout(sconfig.TimeoutPolicy.DefaultTimeout)
	client.SetContentLength(true)

	client.SetTransport(&http.Transport{
		MaxIdleConns:       MaxIdleConnsInClientPool,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: true,
	})

	// retry policy
	if sconfig.RetryPolicy.EnableRetry {
		client.SetRetryCount(sconfig.RetryPolicy.MaxRetryTimes)
		client.SetRetryWaitTime(sconfig.RetryPolicy.DefaultRetryDelay)
		client.SetRetryMaxWaitTime(sconfig.RetryPolicy.MaxRetryDelay)
		// TODO(Ping Zhang): we may need to handle stream and non-stream requests separately,
		// in a very fine-grained way.
		client.AddRetryCondition(
			func(r *resty.Response, err error) bool {
				if r != nil {
					for _, code := range sconfig.RetryPolicy.RetriableStatusCodes {
						if r.StatusCode() == code {
							return true
						}
					}
				}
				// When there is no new replicas for retry, stop retrying.
				if r == nil && err != nil && err.Error() == "NoNewReplicasForRetry" {
					return false
				}
				// TODO (Ping Zhang): fine-grained control of retry conditions
				// retry on other errors: e.g., connection error, reset, etc.
				return err != nil
			})
		client.OnBeforeRequest(
			func(c *resty.Client, req *resty.Request) error {
				// Retry only if the request is not the first attempt
				logger.Log.Debugf("Request Attempt: %d", req.Attempt)
				if req.Attempt <= 1 {
					return nil
				}

				reqPath := req.RawRequest.URL.Path
				oldReplica := req.RawRequest.URL.Host
				// I. Release the request number on the replica once the request is failed inner max retry.
				defer logger.Log.Infof("releasing request number on failed replica: %s", oldReplica)
				defer lbpolicy.UpdateAfterResponse(oldReplica)

				// Redirect the request to another replica
				inferRequestID, _ := req.Context().Value("inferRequestID").(string)
				logger.Log.Infof("retring Request: %s", inferRequestID)
				logger.Log.Infof("The original replica URL is %s, path is %s", oldReplica, reqPath)

				newURL := lbpolicy.SelectReplicaForRetry(inferRequestID, oldReplica)
				if newURL == "" {
					logger.Log.Warn("Retry Error: No ready replicas for retry")
					return errors.New("NoNewReplicasForRetry")
				}
				if !utils.StartsWithHTTP(newURL) {
					newURL = "http://" + newURL
				}
				req.URL = newURL + reqPath
				logger.Log.Infof("Request %s is sent to %s for retry", inferRequestID, req.URL)
				return nil
			})
	} else {
		// We keep this to manually disbale retry
		client.SetRetryCount(0)
	}

	// TODO (Ping Zhang): the request is sent to the selected replica successfully,
	// but the response type is uncertain. the OnAfterResponse hooks will be executed
	// only when SetDoNotParseResponse(true).

	/***
	client.OnAfterResponse(func(c *resty.Client, resp *resty.Response) error {
		replica := resp.Request.RawRequest.URL.Host
		logger.Log.Infof("releasing request number on replica: %s", replica)
		// Each time a request finished, success or failure,
		// update the number of requests for the selected replica.
		lbpolicy.UpdateAfterResponse(replica)
		return nil
	})
	***/

	// TODO(Ping Zhang): the request sent process is failed due to some reasons,
	// e.g., connection error, reset, etc.

	/***
	client.OnError(func(req *resty.Request, err error) {
		logger.Log.Info("OnError Callback is called ======")
		if v, ok := err.(*resty.ResponseError); ok {
			// v.Response contains the last response from the server
			// v.Err contains the original error
			logger.Log.Infof("OnError Response Status Code: %d", v.Response.StatusCode())
		}
		// Log the error, increment a metric, etc...
		lbpolicy.UpdateAfterResponse(req.RawRequest.URL.Host)
	})
	***/

	return client
}

// Create a scheduler instance
func NewBaichuanScheduler(sconfig *config.SchedulerConfig) *BaichuanScheduler {
	// Set the gin mode to release mode
	// uncomment to debug
	gin.SetMode(gin.ReleaseMode)

	// Disable Gin's default logging
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard

	balancer := &BaichuanScheduler{
		appServer:        gin.New(),
		loadBalancerPort: sconfig.LBPort,
		schedulerConfig:  sconfig,
		request_total: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "request_counts",
				Help: "the total nums of requests",
			}),
	}

	// Initialize the load balancing policy
	switch sconfig.LBPolicy {
	case "least-number-of-requests":
		balancer.loadBalancingPolicy = policy.NewLeastNumberOfRequestsPolicy()
	case "round-robin":
		balancer.loadBalancingPolicy = policy.NewRoundRobinPolicy()
	case "cache-aware":
		balancer.loadBalancingPolicy = policy.NewCacheAwarePolicy()
	}

	// Create and configure resty client
	balancer.appClient = configureRestyClient(balancer.loadBalancingPolicy, sconfig)

	balancer.appServer.GET("/health", balancer.handleHealthCheck)
	balancer.appServer.Any("/", balancer.handleRequest)
	balancer.appServer.GET("/metrics", balancer.handleMetrics)
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
			logger.Log.Debugf("Received ready replicas: %v", ReadyEndpoints)
			logger.Log.Infof("Received ready replicas count: %d", len(ReadyEndpoints))
			lb.loadBalancingPolicy.SetReadyReplicas(ReadyEndpoints)
		case <-ticker.C:
			// just to keep the loop running
		}
	}
}

// handleRequest manages incoming requests by proxying them to service replicas.
func (lb *BaichuanScheduler) handleRequest(c *gin.Context) {
	session_id := c.GetHeader("SESSION_ID")
	request_id := c.GetHeader("REQUEST_ID")
	// add request counts and update metrics
	lb.request_total.Inc()

	inferRequest := types.InferRequest{
		// TODO(sunyijia): get request_id from header
		RequestID: request_id,
		SessionID: session_id,
	}

	logger.Log.Info("Received request, session_id: ", session_id, ", request_id: ", request_id)

	// adapted to scale to zero, when none ready replica, wait to scale up
	for count := 0; count <= 1200; count++ {
		readyReplicas := lb.loadBalancingPolicy.GetReadyReplicas()
		if len(readyReplicas) != 0 {
			logger.Log.Debugf("There's %d ready replicas. request_id: %s", len(readyReplicas), request_id)
			break
		}
		logger.Log.Debugf("None ready replicas. Wait to scale up. request_id: %s", request_id)
		time.Sleep(1 * time.Second)
	}

	var readyReplicaURL string
	for count := 0; count <= 120; count++ {
		readyReplicaURL = lb.loadBalancingPolicy.SelectReplica(&inferRequest)
		if readyReplicaURL != "" {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if readyReplicaURL == "" {
		logAndRespondError(c, http.StatusServiceUnavailable, "No ready replicas", nil, request_id)
		return
	}

	path := c.Request.URL.Path
	targetURL := formatURL(readyReplicaURL, path)

	restyRequest := lb.appClient.R().
		EnableTrace().
		SetDoNotParseResponse(true).
		SetBody(c.Request.Body).
		SetContext(context.WithValue(c, "inferRequestID", inferRequest.RequestID)) // Save for retry

	// Copy headers
	for key, values := range c.Request.Header {
		for _, value := range values {
			restyRequest.SetHeader(key, value)
		}
	}

	logger.Log.Infof("Proxying request to %s, request_id: %s, session_id: %s", targetURL, request_id, session_id)
	resp, err := restyRequest.Execute(c.Request.Method, targetURL)
	if resp == nil {
		logAndRespondError(c, http.StatusInternalServerError, "Failed to proxy request", err, request_id)
		return
	}
	/***
	II. Release the request number on the replica once:
	     (1) the request is finished successfully.
		 (2) the request failed even after max retries.
	***/
	defer lb.loadBalancingPolicy.UpdateAfterResponse(resp.Request.RawRequest.URL.Host)

	if err != nil || !resp.IsSuccess() {
		logger.Log.Errorf("Failed to proxy request, status code: %s", resp.Status())
		logAndRespondError(c, http.StatusInternalServerError, "Failed to proxy request", err, request_id)
		return
	}

	lb.setResponseHeaders(c, resp.RawResponse)
	lb.traceInfoForDebug(resp.Request.TraceInfo())

	rawBody := resp.RawBody()
	// Since we are using SetDoNotParseResponse(true),
	// we need to close the body manually
	defer rawBody.Close()

	if strings.HasSuffix(path, "/generate_stream") {
		// Stream response directly to client
		// Create a flusher
		flusher, ok := c.Writer.(http.Flusher)
		if !ok {
			logAndRespondError(c, http.StatusInternalServerError, "Failed to stream proxied response", errors.New("response writer does not support flushing"), request_id)
			return
		}

		// Stream the response and flush
		buf := make([]byte, 32*1024) // TODO(Ping Zhang): tune the buffer size
		for {
			n, err := rawBody.Read(buf)
			if n > 0 {
				_, writeErr := c.Writer.Write(buf[:n])
				if writeErr != nil {
					logAndRespondError(c, http.StatusInternalServerError, "Failed to write proxied response", writeErr, request_id)
					return
				}
				flusher.Flush() // Flush the buffer to the client
			}
			if err != nil {
				if err != io.EOF {
					logAndRespondError(c, http.StatusInternalServerError, "Failed to read proxied response", err, request_id)
					return
				}
				break
			}
		}
		logger.Log.Infof("Streamed response to client successfully, statuscode: %d, request_id: %s", resp.StatusCode(), request_id)
	} else {
		// For non-stream, read all and then send
		var responseBytes bytes.Buffer
		_, err := io.Copy(&responseBytes, rawBody)
		if err != nil {
			logAndRespondError(c, http.StatusInternalServerError, "Failed to read proxied response", err, request_id)
			return
		}

		c.Data(resp.StatusCode(), resp.Header().Get("Content-Type"), responseBytes.Bytes())
		logger.Log.Infof("Sent response to client successfully, statuscode: %d, request_id: %s", resp.StatusCode(), request_id)
	}
}

/***
// StartCollectingQueueSize starts a goroutine to collect the queue size of each replica.
// This function is used for Cache-Aware load balancing policy.
// We comment it out right now for UT purpose.
// TODO (Ping Zhang): Write UT for this function.

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
					logger.Log.Errorf("Error collecting from %s: %v", url, err)
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
		logger.Log.Infof("Round took %v", roundEnd.Sub(roundStart).Milliseconds())
		// print the queue size for each replica
		metrics.PrintSortedTgiMetric(collector)
	}
}
***/

func (lb *BaichuanScheduler) Run() {
	go endpointwatcher.WatchEndpoints(lb.schedulerConfig)
	go lb.syncReplicas()

	logger.Log.Infof("Baichuan scheduler started on http://0.0.0.0:%d", lb.loadBalancerPort)
	logger.Log.Infof("Baichuan scheduler is using %s load balancing policy", lb.schedulerConfig.LBPolicy)
	lb.appServer.Run(fmt.Sprintf(":%d", lb.loadBalancerPort))
}

func (lb *BaichuanScheduler) setResponseHeaders(c *gin.Context, resp *http.Response) {
	if resp == nil {
		logger.Log.Warn("Response is nil")
		return
	}
	c.Writer.WriteHeader(resp.StatusCode)
	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
}

func (lb *BaichuanScheduler) traceInfoForDebug(ti resty.TraceInfo) {
	logger.Log.Debug("======================= Request Trace Info: Start =====================")
	logger.Log.Debug("  DNSLookup     :", ti.DNSLookup)
	logger.Log.Debug("  ConnTime      :", ti.ConnTime)
	logger.Log.Debug("  TCPConnTime   :", ti.TCPConnTime)
	logger.Log.Debug("  ServerTime    :", ti.ServerTime)
	logger.Log.Debug("  ResponseTime  :", ti.ResponseTime)
	logger.Log.Debug("  TotalTime     :", ti.TotalTime)
	logger.Log.Debug("  IsConnReused  :", ti.IsConnReused)
	logger.Log.Debug("  IsConnWasIdle :", ti.IsConnWasIdle)
	logger.Log.Debug("  ConnIdleTime  :", ti.ConnIdleTime)
	logger.Log.Debug("  RequestAttempt:", ti.RequestAttempt)
	logger.Log.Debug("  RemoteAddr    :", ti.RemoteAddr.String())
	logger.Log.Debug("======================== Request Trace Info: End  ====================")
}

func (lb *BaichuanScheduler) handleHealthCheck(c *gin.Context) {
	c.Header("Content-Type", "text/plain")
	numOfReadyReplicas := len(lb.loadBalancingPolicy.GetReadyReplicas())
	if numOfReadyReplicas > 0 {
		c.String(http.StatusOK, "OK")
	} else {
		logger.Log.Error("No ready replicas available")
		c.String(http.StatusServiceUnavailable, "No ready replicas available")
	}
}

func (lb *BaichuanScheduler) handleMetrics(c *gin.Context) {
	h := promhttp.Handler()
	h.ServeHTTP(c.Writer, c.Request)
}

// formatURL ensures the URL is prefixed with "http://" if not already present.
func formatURL(baseURL, path string) string {
	if !utils.StartsWithHTTP(baseURL) {
		baseURL = "http://" + baseURL
	}
	return baseURL + path
}

// logAndRespondError logs the error and sends a JSON response with the specified status and error message.
func logAndRespondError(c *gin.Context, status int, message string, err error, request_id string) {
	if err != nil {
		logger.Log.Errorf("%s: %v, request_id: %s", message, err, request_id)
		c.JSON(status, gin.H{"error": message + ": " + err.Error()})
	} else {
		logger.Log.Errorf("%s, request_id: %s", message, request_id)
		c.JSON(status, gin.H{"error": message})
	}
}
