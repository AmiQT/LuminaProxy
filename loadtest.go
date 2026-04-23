//go:build ignore

package main

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	url := "http://localhost:8080/lumina-metrics" // Pertukaran ke payload berat (~1.5MB JSON)
	totalRequests := 10000                 // 20 Ribu Requests untuk demo kelajuan ekstrem!
	maxWorkers := 60                     // Tambah pekerja untuk hit throughput tinggi

	fmt.Printf("Menembak %d requests ke LuminaProxy dengan %d pekerja (workers) serentak...\n", totalRequests, maxWorkers)
	start := time.Now()

	// Buat channel untuk agihkan kerja
	jobs := make(chan int, totalRequests)
	for i := 0; i < totalRequests; i++ {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	var successCount int32
	var rateLimitCount int32
	var errorCount int32
	
	// Custom HTTP client untuk guna semula TCP Connection (Keep-Alive)
	// Sangat penting untuk test 1 Juta ping
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        maxWorkers,
			MaxIdleConnsPerHost: maxWorkers,
		},
		Timeout: 10 * time.Second,
	}

	for w := 0; w < maxWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				resp, err := client.Get(url)
				if err == nil {
					if resp.StatusCode == 200 {
						atomic.AddInt32(&successCount, 1)
					} else if resp.StatusCode == 429 {
						atomic.AddInt32(&rateLimitCount, 1)
					}
					resp.Body.Close()
				} else {
					atomic.AddInt32(&errorCount, 1)
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Printf("Selesai! Berjaya proses %d requests dalam masa %v\n", totalRequests, duration)
	fmt.Printf("Berjaya Masuk (200 OK): %d\n", successCount)
	fmt.Printf("Di-Block Bouncer (429 Too Many Requests): %d\n", rateLimitCount)
	fmt.Printf("Ralat Jalanan (TCP Drop / OS Limit): %d\n", errorCount)
	fmt.Printf("Kelajuan (Throughput): %.0f Requests Per Second (RPS)\n", float64(totalRequests)/duration.Seconds())
}
