package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPIgnoresSpoofedForwardedHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.10:1234"
	r.Header.Set("X-Forwarded-For", "10.0.0.99")
	if got := clientIP(r, true); got != "203.0.113.10" {
		t.Fatalf("clientIP=%q, want direct peer", got)
	}
}

func TestClientIPTrustsPrivateProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.5:1234"
	r.Header.Set("X-Forwarded-For", "192.168.1.5, 198.51.100.7")
	if got := clientIP(r, true); got != "198.51.100.7" {
		t.Fatalf("clientIP=%q, want rightmost forwarded hop", got)
	}
}

func TestClientIPIgnoresSpoofedLeftmostHop(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.5:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.7")
	if got := clientIP(r, true); got != "198.51.100.7" {
		t.Fatalf("clientIP=%q, want rightmost forwarded hop", got)
	}
}
