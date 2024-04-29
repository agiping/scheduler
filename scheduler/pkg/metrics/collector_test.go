package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCollect(t *testing.T) {
	// Create a test server that returns a fixed response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			w.Write([]byte(`

# HELP tgi_queue_size The number of requests in the queue
tgi_queue_size 10

# HELP tgi_request_queue_duration_sum The sum of the request queue duration
tgi_request_queue_duration_sum 100
tgi_request_queue_duration_count 5

		`))
		}
	}))
	defer server.Close()

	// Create a TgiMetricCollector with a real http.Client
	collector := NewTgiMetricCollector(http.DefaultClient)

	// Call the Collect method
	err := collector.Collect(server.URL)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// Check the collected metrics
	metrics, ok := collector.ReplicaMetrics.Load(server.URL)
	if !ok {
		t.Fatalf("expected metrics for URL %s, got none", server.URL)
	}

	tgiMetrics, ok := metrics.(TgiMetrics)
	if !ok {
		t.Fatalf("expected TgiMetrics, got %T", metrics)
	}

	if tgiMetrics.QueueSize != 10 {
		t.Errorf("expected QueueSize 10, got %d", tgiMetrics.QueueSize)
	}
	if tgiMetrics.QueueTime != 20000.0000 {
		t.Errorf("expected QueueTime 20, got %f", tgiMetrics.QueueTime)
	}
}
