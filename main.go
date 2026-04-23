package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	// "strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

// 1. Struktur Data dengan Stale & Expires
type CacheItem struct {
	Body        []byte
	ContentType string
	StatusCode  int
	CachedAt    time.Time
	StaleAt     time.Time // Bila data ni dah basi (kena refresh secara sembunyi)
	ExpiresAt   time.Time // Bila data ni dah mati terus (kena MISS)
}

// 2. Cache guna LRU + Metrics
type LuminaCache struct {
	lru          *lru.Cache[string, CacheItem]
	Hits         uint64
	Misses       uint64
	StaleRefresh uint64
}

func NewLuminaCache(maxItems int) *LuminaCache {
	// LRU secara automatik menendang item lama bila RAM nak penuh!
	cache, _ := lru.New[string, CacheItem](maxItems)
	return &LuminaCache{lru: cache}
}

// Semak status Cache: HIT, STALE, atau MISS
func (c *LuminaCache) GetStatus(key string) (CacheItem, string) {
	item, found := c.lru.Get(key)
	if !found {
		return CacheItem{}, "MISS"
	}
	if time.Now().After(item.ExpiresAt) {
		c.lru.Remove(key)
		return CacheItem{}, "MISS"
	}
	if time.Now().After(item.StaleAt) {
		return item, "STALE" // Basi, tapi masih boleh makan!
	}
	return item, "HIT"
}

// 3. Rate Limiter: Bouncer Pintu (Elak DDoS dari 1 IP)
type IPRateLimiter struct {
	ips map[string]*rate.Limiter
	mu  sync.RWMutex
	r   rate.Limit
	b   int
}

func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	return &IPRateLimiter{
		ips: make(map[string]*rate.Limiter),
		r:   r,
		b:   b,
	}
}

func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.RLock()
	limiter, exists := i.ips[ip]
	i.mu.RUnlock()

	if !exists {
		i.mu.Lock()
		limiter, exists = i.ips[ip]
		if !exists {
			limiter = rate.NewLimiter(i.r, i.b) // Contoh: 100 limit, 50 burst
			i.ips[ip] = limiter
		}
		i.mu.Unlock()
	}
	return limiter
}

var serverStartTime = time.Now()

func main() {
	defaultUpstream := "https://jsonplaceholder.typicode.com"
	portFlag := flag.String("port", "8080", "Proxy listening port")
	ttlFlag := flag.Int("ttl", 60, "Cache TTL (Expires) in seconds")
	staleFlag := flag.Int("stale", 30, "Cache Stale in seconds")
	flag.Parse()

	upstreamURL, err := url.Parse(defaultUpstream)
	if err != nil {
		log.Fatalf("invalid upstream URL: %v", err)
	}

	ttl := time.Duration(*ttlFlag) * time.Second
	staleTTL := time.Duration(*staleFlag) * time.Second

	// Setup LRU Max 2000 item (Cukup untuk kawal RAM)
	cache := NewLuminaCache(2000)

	// Setup Rate Limiter (Max 10000 request sesaat per IP, Burst 15000)
	limiter := NewIPRateLimiter(10000, 15000)

	proxy := httputil.NewSingleHostReverseProxy(upstreamURL)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = upstreamURL.Host
	}

	var requestGroup singleflight.Group

	// Helper function untuk tarik dari upstream
	fetchAndUpdateCache := func(r *http.Request, cacheKey string) (CacheItem, error) {
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, r)

		res := rec.Result()
		body := rec.Body.Bytes()

		item := CacheItem{
			Body:        body,
			ContentType: res.Header.Get("Content-Type"),
			StatusCode:  res.StatusCode,
			CachedAt:    time.Now(),
			StaleAt:     time.Now().Add(staleTTL),
			ExpiresAt:   time.Now().Add(ttl),
		}

		if res.Header.Get("Set-Cookie") == "" && res.StatusCode >= 200 && res.StatusCode < 300 && len(body) > 0 {
			cache.lru.Add(cacheKey, item)
		}
		return item, nil
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// start := time.Now()

		// 1. Rate Limiting Check
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !limiter.GetLimiter(ip).Allow() {
			http.Error(w, "429 Too Many Requests - Sabar bang!", http.StatusTooManyRequests)
			return
		}

		// 2. Metrics Endpoint
		if r.URL.Path == "/lumina-metrics" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"uptime_seconds":    time.Since(serverStartTime).Seconds(),
				"total_cache_items": cache.lru.Len(),
				"total_hits":        atomic.LoadUint64(&cache.Hits),
				"total_misses":      atomic.LoadUint64(&cache.Misses),
				"stale_refreshes":   atomic.LoadUint64(&cache.StaleRefresh),
			})
			return
		}

		if r.Method != http.MethodGet {
			w.Header().Set("X-Lumina-Cache", "MISS")
			proxy.ServeHTTP(w, r)
			return
		}

		cacheKey := upstreamURL.String() + ":" + r.URL.RequestURI()
		item, status := cache.GetStatus(cacheKey)

		// 3. HIT & STALE-WHILE-REVALIDATE Logic
		if status == "HIT" || status == "STALE" {
			w.Header().Set("Content-Type", item.ContentType)
			w.Header().Set("X-Lumina-Cache", status)

			if status == "HIT" {
				atomic.AddUint64(&cache.Hits, 1)
				// fmt.Printf("[HIT  ] %s %s | %d µs\n", r.Method, r.URL.RequestURI(), time.Since(start).Microseconds())
			} else {
				atomic.AddUint64(&cache.StaleRefresh, 1)
				// fmt.Printf("[STALE] %s %s | serving old data & refreshing background! | %d µs\n", r.Method, r.URL.RequestURI(), time.Since(start).Microseconds())

				// Stale: Jalan background fetch supaya user seterusnya dapat data baru
				go func() {
					bgReq := r.Clone(context.Background())
					requestGroup.Do("bg:"+cacheKey, func() (interface{}, error) {
						return fetchAndUpdateCache(bgReq, cacheKey)
					})
				}()
			}

			w.WriteHeader(item.StatusCode)
			_, _ = w.Write(item.Body)
			return
		}

		// 4. MISS dengan Singleflight
		v, err, shared := requestGroup.Do(cacheKey, func() (interface{}, error) {
			atomic.AddUint64(&cache.Misses, 1)
			return fetchAndUpdateCache(r, cacheKey)
		})

		if err != nil {
			http.Error(w, "Proxy Error", http.StatusInternalServerError)
			return
		}

		item = v.(CacheItem)
		w.Header().Set("Content-Type", item.ContentType)

		if shared {
			w.Header().Set("X-Lumina-Cache", "HIT-SHARED")
			atomic.AddUint64(&cache.Hits, 1)
			// fmt.Printf("[SAVED] %s %s | %d ms\n", r.Method, r.URL.RequestURI(), time.Since(start).Milliseconds())
		} else {
			w.Header().Set("X-Lumina-Cache", "MISS")
			// fmt.Printf("[MISS ] %s %s | %d ms\n", r.Method, r.URL.RequestURI(), time.Since(start).Milliseconds())
		}

		w.WriteHeader(item.StatusCode)
		_, _ = w.Write(item.Body)
	})

	server := &http.Server{
		Addr:    ":" + *portFlag,
		Handler: handler,
	}

	// 5. Graceful Shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		fmt.Printf("\nGo-Lumina Enterprise running on http://localhost:%s\n", *portFlag)
		fmt.Printf("Keupayaan: LRU Cache (Mem Berhad), Stale-While-Revalidate, Rate Limiter, Graceful Shutdown\n\n")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Tunggu isyarat Ctrl+C
	<-ctx.Done()
	fmt.Println("\n[SIGINT] Diterima. Memulakan \"Graceful Shutdown\"...")
	fmt.Println("Menunggu request yang sedang berjalan selesai (Max 5 saat)...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Masalah semasa shutdown: %v", err)
	}

	fmt.Println("LuminaProxy ditutup dengan selamat. Bye!")
}
