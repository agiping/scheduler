package metrics

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// The interval at which to collect the queue size from each replica
	CollectionInterval = 2 * time.Second
	// The maximum number of concurrent requests to make for metrics collection
	MaxConcurrency = 50
)

type TgiQueueSizeCollector struct {
	Client *http.Client
	// We use a sync.Map to store the queue size for each replica URL.
	ReplicaQueueSize sync.Map
}

func NewTgiQueueSizeCollector(client *http.Client) *TgiQueueSizeCollector {
	return &TgiQueueSizeCollector{
		Client: client,
	}
}

func (tqs *TgiQueueSizeCollector) Collect(replicaUrl string) error {
	getUrl := rebuildUrls(replicaUrl)
	resp, err := tqs.Client.Get(getUrl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// TODO (Ping Zhang): We may want to limit the size of data read from the response body
	// to improve performance.
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "tgi_queue_size") {
			parts := strings.Split(line, " ")
			if len(parts) < 2 {
				continue
			}
			queueSize, err := strconv.Atoi(parts[1])
			if err != nil {
				return err
			}
			// update the queue size only if it has changed
			if qsize, exist := tqs.ReplicaQueueSize.Load(replicaUrl); !exist || qsize.(int) != queueSize {
				tqs.ReplicaQueueSize.Store(replicaUrl, queueSize)
			}
			break
		}
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

// PrintSortedQueueSizes prints the queue size in a table format.
func PrintSortedQueueSizes(collector *TgiQueueSizeCollector) {
	fmt.Println()
	var replicas []string
	queueSizes := make(map[string]int)

	collector.ReplicaQueueSize.Range(func(key, value interface{}) bool {
		k := key.(string)
		v := value.(int)
		replicas = append(replicas, k)
		queueSizes[k] = v
		return true
	})

	fmt.Printf("%-50s %s\n", "Replica URL", "Queue Size")
	fmt.Println(strings.Repeat("-", 60))

	for _, replica := range replicas {
		fmt.Printf("%-50s %d\n", replica, queueSizes[replica])
	}
}
