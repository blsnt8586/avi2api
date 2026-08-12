package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:18081", "AIV2API base URL")
	apiKey := flag.String("api-key", "", "test API key")
	requests := flag.Int("requests", 100, "number of requests")
	concurrency := flag.Int("concurrency", 100, "concurrent workers")
	flag.Parse()
	if *apiKey == "" || *requests < 1 || *concurrency < 1 {
		fmt.Fprintln(os.Stderr, "api-key, positive requests and positive concurrency are required")
		os.Exit(2)
	}
	if *concurrency > *requests {
		*concurrency = *requests
	}
	transport := &http.Transport{MaxIdleConns: *concurrency, MaxIdleConnsPerHost: *concurrency, MaxConnsPerHost: *concurrency}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer transport.CloseIdleConnections()

	type sample struct {
		status   int
		duration time.Duration
		err      string
	}
	jobs := make(chan int)
	results := make(chan sample, *requests)
	runID := time.Now().UnixNano()
	var wg sync.WaitGroup
	for worker := 0; worker < *concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				payload, _ := json.Marshal(map[string]any{"model": "gpt-image-2", "prompt": fmt.Sprintf("load fixture %d", index), "size": "1024x1024", "quality": "low", "n": 1, "response_format": "url"})
				req, err := http.NewRequest(http.MethodPost, *baseURL+"/v1/images/generations", bytes.NewReader(payload))
				if err != nil {
					results <- sample{err: err.Error()}
					continue
				}
				req.Header.Set("Authorization", "Bearer "+*apiKey)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Idempotency-Key", fmt.Sprintf("admission-%d-%d", runID, index))
				started := time.Now()
				resp, err := client.Do(req)
				duration := time.Since(started)
				if err != nil {
					results <- sample{duration: duration, err: err.Error()}
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				results <- sample{status: resp.StatusCode, duration: duration}
			}
		}()
	}
	started := time.Now()
	go func() {
		for index := 0; index < *requests; index++ {
			jobs <- index
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	statusCounts := map[int]int{}
	errorCounts := map[string]int{}
	durations := make([]time.Duration, 0, *requests)
	var completed atomic.Int64
	for result := range results {
		completed.Add(1)
		if result.err != "" {
			errorCounts[result.err]++
		} else {
			statusCounts[result.status]++
		}
		durations = append(durations, result.duration)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	percentile := func(p float64) int64 {
		if len(durations) == 0 {
			return 0
		}
		index := int(float64(len(durations)-1) * p)
		return durations[index].Milliseconds()
	}
	report := map[string]any{
		"requests": completed.Load(), "concurrency": *concurrency,
		"elapsed_ms": time.Since(started).Milliseconds(), "status_counts": statusCounts,
		"errors": errorCounts, "p50_ms": percentile(0.50), "p95_ms": percentile(0.95),
		"p99_ms": percentile(0.99), "max_ms": percentile(1),
	}
	encoded, _ := json.Marshal(report)
	fmt.Println(string(encoded))
}
