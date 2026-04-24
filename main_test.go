package main

import (
	"testing"
	"time"
)

func TestLoadBalancer_Next(t *testing.T) {
	urls := []string{"http://server1:8080", "http://server2:8080", "http://server3:8080"}
	lb := NewLoadBalancer(urls)

	// Since LoadBalancer starts with idx=0 but increments before returning the first time,
	// the first Next() returns urls[1], second returns urls[2], third returns urls[0]
	for i := 0; i < len(urls)*2; i++ { // Pusing 2 kali
		expected := urls[(i+1)%len(urls)]
		got := lb.Next()
		if got.URL.String() != expected {
			t.Errorf("Pusingan %d: Jangkaan %q, Dapat %q", i, expected, got.URL.String())
		}
	}
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	threshold := 3
	timeout := 100 * time.Millisecond // Masa yang pendek untuk ujian
	cb := NewCircuitBreaker(threshold, timeout)

	// Ujian 1: Keadaan awal sepatutnya CLOSED
	if cb.state != StateClosed {
		t.Errorf("Keadaan awal sepatutnya CLOSED, dapat %v", cb.state)
	}

	// Ujian 2: Rakam ralat di bawah threshold, keadaan patut kekal CLOSED
	cb.RecordFailure()
	cb.RecordFailure()
	if !cb.AllowRequest() {
		t.Errorf("Request sepatutnya dibenarkan sebelum threshold dicapai")
	}

	// Ujian 3: Capai threshold, keadaan patut bertukar OPEN
	cb.RecordFailure()
	if cb.AllowRequest() {
		t.Errorf("Request TIDAK sepatutnya dibenarkan selepas threshold dicapai (Circuit patut OPEN)")
	}

	// Ujian 4: Selepas timeout, keadaan patut bertukar HALF-OPEN
	time.Sleep(timeout + 50*time.Millisecond) // Tunggu sehingga timeout tamat
	if !cb.AllowRequest() {
		t.Errorf("Request sepatutnya dibenarkan selepas timeout (Circuit patut HALF-OPEN)")
	}
	if cb.state != StateHalfOpen {
		t.Errorf("Keadaan sepatutnya HALF-OPEN, dapat %v", cb.state)
	}

	// Ujian 5: Kejayaan semasa HALF-OPEN patut pulihkan keadaan ke CLOSED
	cb.RecordSuccess()
	if cb.state != StateClosed {
		t.Errorf("Keadaan sepatutnya CLOSED selepas kejayaan, dapat %v", cb.state)
	}
}
