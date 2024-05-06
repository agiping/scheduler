package metrics

import (
	"bufio"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// The interval at which to collect metrics from each replica
	CollectionInterval = 2 * time.Second
	// The maximum number of concurrent requests to make for metrics collection
	MaxConcurrency = 50
)

type TgiMetrics struct {
	QueueSize int     // The number of requests in the queue
	QueueTime float64 // The average time a request spends in the queue, in milliseconds.
}

type TgiMetricCollector struct {
	Client         *http.Client
	ReplicaMetrics sync.Map // key: replica URL, value: TgiMetrics
}

func NewTgiMetricCollector(client *http.Client) *TgiMetricCollector {
	return &TgiMetricCollector{
		Client: client,
	}
}

func (tqs *TgiMetricCollector) Collect(replicaUrl string) error {
	getUrl := rebuildUrls(replicaUrl)
	resp, err := tqs.Client.Get(getUrl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var qSize int
	var qTimeSum float64
	var qCount int
	var avgQueuetime float64
	var metricCount int

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key, value := fields[0], fields[1]

		switch {
		case key == "tgi_queue_size":
			if parsedInt, err := strconv.Atoi(value); err == nil {
				qSize = parsedInt
				metricCount++
			} else {
				return err
			}
		case key == "tgi_request_queue_duration_sum":
			if parsedFloat, err := strconv.ParseFloat(value, 64); err == nil {
				qTimeSum = parsedFloat
				metricCount++
			} else {
				return err
			}
		case key == "tgi_request_queue_duration_count":
			if parsedInt, err := strconv.Atoi(value); err == nil {
				qCount = parsedInt
				metricCount++
			} else {
				return err
			}
		}

		if qCount > 0 {
			avgQueuetime = qTimeSum / float64(qCount) * 1000      // convert from seconds to milliseconds
			avgQueuetime = math.Round(avgQueuetime*10000) / 10000 // round to 4 decimal places
		}

		if metricCount == 3 {
			tqs.ReplicaMetrics.Store(replicaUrl, TgiMetrics{QueueSize: qSize, QueueTime: avgQueuetime})
			break
		}
	}

	if metricCount != 3 {
		return fmt.Errorf("expected 3 metrics, got %d", metricCount)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

// collector expects the metrics endpoint to be at /metrics of tgi
func rebuildUrls(url string) string {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	return url + "/metrics"
}

// PrintSortedTgiMetric prints the queue state in a table format.
func PrintSortedTgiMetric(collector *TgiMetricCollector) {
	log.Println()
	var replicas []string
	TgiMetric := make(map[string]TgiMetrics)

	collector.ReplicaMetrics.Range(func(key, value interface{}) bool {
		k := key.(string)
		v := value.(TgiMetrics)
		replicas = append(replicas, k)
		TgiMetric[k] = v
		return true
	})

	log.Printf("%-50s %s\n", "Replica IP", "Tgi Queue State")
	log.Println(strings.Repeat("-", 90))

	for _, replica := range replicas {
		metric := TgiMetric[replica]
		log.Printf("%-50s QueueSize: %d    AvgQueueTime(ms): %.4f\n", replica, metric.QueueSize, metric.QueueTime)
	}
}
