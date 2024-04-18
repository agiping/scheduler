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
	collectionInterval = 2 * time.Second
	// The maximum number of concurrent requests to make for metrics collection
	maxConcurrency = 50
)

type TgiQueueSizeCollector struct {
	Client *http.Client
	// We use a sync.Map to store the queue size for each replica URL
	ReplicaQueueSize sync.Map
}

func NewTgiQueueSizeCollector(client *http.Client) *TgiQueueSizeCollector {
	return &TgiQueueSizeCollector{
		Client: client,
	}
}

func (tqs *TgiQueueSizeCollector) Collect(replicaUrl string) error {
	replicaUrl = rebuildUrls(replicaUrl)
	resp, err := tqs.Client.Get(replicaUrl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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
			tqs.ReplicaQueueSize.Store(replicaUrl, queueSize)
			break
		}
	}
	return nil
}

func printSortedQueueSizes(collector *TgiQueueSizeCollector) {
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

// collector expects the metrics endpoint to be at /metrics of tgi
func rebuildUrls(url string) string {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	return url + "/metrics"
}

func StartCollecting(replicaUrls []string) {
	client := &http.Client{}
	collector := NewTgiQueueSizeCollector(client)
	ticker := time.NewTicker(collectionInterval)
	defer ticker.Stop()

	// limit the number of concurrent requests
	concurrencyControl := make(chan struct{}, maxConcurrency)

	for {
		<-ticker.C
		roundStart := time.Now()
		var wg sync.WaitGroup
		for _, url := range replicaUrls {
			wg.Add(1)
			go func(url string) {
				defer wg.Done()
				// obtain a permit
				concurrencyControl <- struct{}{}
				if err := collector.Collect(url); err != nil {
					fmt.Printf("Error collecting from %s: %v\n", url, err)
				}
				// release the permit
				<-concurrencyControl
			}(url)
		}
		// wait for all requests to finish
		wg.Wait()
		roundEnd := time.Now()
		fmt.Printf("Round took %v\n", roundEnd.Sub(roundStart).Milliseconds())
		// print the queue size for each replica
		printSortedQueueSizes(collector)
	}
}

// func main() {
// 	replicaUrls := []string{
// 		"172.20.120.212:80",
// 		"172.20.154.177:80",
// 		"172.20.156.61:80",
// 	}
// 	StartCollecting(replicaUrls)
// }
