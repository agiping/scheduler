package utils

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ReadyPodIPsChan = make(chan []string)

// TODO(Ping Zhang): Simplicity above all.
// To avoid wired concurrent behavior, we might consider using one channel to pass endpoints of one service.
var ReadyEndpointsChan = make(chan []string)

type MetricsAggregator interface {
	Add(request *http.Request) // We keep the incoming request here in case we need it for advanced load balancing strategies
	Clear()
	ToMap() map[string]interface{}
	String() string
}

// RequestAggregator is a struct that implements the RequestsAggregator interface.
type RequestTimestamp struct {
	mu         sync.Mutex
	timestamps []time.Time
}

// NewRequestTimestamp creates a new RequestTimestamp.
func NewRequestTimestamp() *RequestTimestamp {
	return &RequestTimestamp{
		timestamps: make([]time.Time, 0),
	}
}

// Add records the current time as the timestamp of a request.
func (rt *RequestTimestamp) Add(request *http.Request) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.timestamps = append(rt.timestamps, time.Now())
}

// Clear resets the list of timestamps.
func (rt *RequestTimestamp) Clear() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.timestamps = []time.Time{}
}

// ToMap returns the timestamps as a map which can be used for JSON serialization or other purposes.
func (rt *RequestTimestamp) ToMap() map[string]interface{} {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	timestamps := make([]int64, len(rt.timestamps))
	for i, t := range rt.timestamps {
		timestamps[i] = t.Unix() // turn into UNIX timestamps (seconds since January 1, 1970).
	}
	return map[string]interface{}{"timestamps": timestamps}
}

// String provides a string representation of the RequestTimestamp, e.g., useful for debugging.
func (rt *RequestTimestamp) String() string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return fmt.Sprintf("RequestTimestamp(timestamps=%v)", rt.timestamps)
}

type TgiQueueState struct {
	mu           sync.Mutex
	avgQueueSize int
	avgQueueTime float64
}

func NewTgiQueueState() *TgiQueueState {
	return &TgiQueueState{
		avgQueueSize: 0,
		avgQueueTime: 0,
	}
}

func (tqs *TgiQueueState) Add(queueSize int, queueTime float64) {
	tqs.mu.Lock()
	defer tqs.mu.Unlock()
	tqs.avgQueueSize = queueSize
	tqs.avgQueueTime = queueTime
}

func (tqs *TgiQueueState) Clear() {
	tqs.mu.Lock()
	defer tqs.mu.Unlock()
	tqs.avgQueueTime = 0
	tqs.avgQueueSize = 0
}

func (tqs *TgiQueueState) ToMap() map[string]interface{} {
	tqs.mu.Lock()
	defer tqs.mu.Unlock()
	return map[string]interface{}{"avgQueueSize": tqs.avgQueueSize, "avgQueueTime": tqs.avgQueueTime}
}

func (tqs *TgiQueueState) String() string {
	tqs.mu.Lock()
	defer tqs.mu.Unlock()
	return fmt.Sprintf("TgiQueueState(avgQueueSize=%d, avgQueueTime=%f)", tqs.avgQueueSize, tqs.avgQueueTime)
}

// ParseRetryStatusCodes takes a comma-separated string of HTTP status codes and returns a slice of ints.
func ParseRetryStatusCodes(codes string) ([]int, error) {
	// Split the string by commas
	parts := strings.Split(codes, ",")
	// Create a slice to hold the integers
	result := make([]int, len(parts))

	for i, part := range parts {
		// Trim any whitespace
		code := strings.TrimSpace(part)
		num, err := strconv.Atoi(code)
		if err != nil {
			// Return the error if the conversion fails
			return nil, err
		}
		result[i] = num
	}

	return result, nil
}

func StartsWithHTTP(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
