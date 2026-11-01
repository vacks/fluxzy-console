package archive

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCollectionLengthGuard(t *testing.T) {
	if err := checkCollectionLen(maxCollectionElements); err != nil {
		t.Fatalf("limit itself should be allowed: %v", err)
	}
	if err := checkCollectionLen(maxCollectionElements + 1); err == nil {
		t.Fatal("expected oversized array length to be rejected")
	}
}

func TestDecodeExchangePost(t *testing.T) {
	ex, err := DecodeExchange(fixture(t, "ex-httpbin-post.mpack"))
	if err != nil {
		t.Fatal(err)
	}

	if ex.ID != 2 || ex.ConnectionID != 2 {
		t.Fatalf("unexpected ids: %+v", ex)
	}
	if ex.Request.Method != "POST" {
		t.Fatalf("method = %q", ex.Request.Method)
	}
	if ex.Request.Authority != "httpbin.org" {
		t.Fatalf("authority = %q", ex.Request.Authority)
	}
	if ex.StatusCode() != 200 {
		t.Fatalf("status = %d", ex.StatusCode())
	}
	if ex.HTTPVersion != "HTTP/1.1" {
		t.Fatalf("http version = %q", ex.HTTPVersion)
	}
	if ex.Metrics.TotalSent == 0 || ex.Metrics.TotalReceived == 0 {
		t.Fatalf("metrics not decoded: %+v", ex.Metrics)
	}
	if ex.Metrics.StartedAt().IsZero() {
		t.Fatal("start time is zero")
	}
	if ex.ContentType() != "application/json" {
		t.Fatalf("content type = %q", ex.ContentType())
	}
	if len(ex.Request.Headers) == 0 || len(ex.Response.Headers) == 0 {
		t.Fatal("headers not decoded")
	}
	if got := header(ex.Request.Headers, "Content-Type"); got != "application/json" {
		t.Fatalf("request content-type = %q", got)
	}
}

func TestDecodeExchangeUnicodeAndTags(t *testing.T) {
	ex, err := DecodeExchange(fixture(t, "ex-tagged-delay.mpack"))
	if err != nil {
		t.Fatal(err)
	}

	if len(ex.Tags) != 1 || ex.Tags[0] != "演示标签" {
		t.Fatalf("tags = %#v", ex.Tags)
	}
	if got := header(ex.Response.Headers, "X-Fluxzy-Test"); got != "中文头" {
		t.Fatalf("unicode header = %q", got)
	}
	if ex.Metrics.Duration() <= 0 {
		t.Fatalf("duration = %v", ex.Metrics.Duration())
	}
	if ex.Pending {
		t.Fatal("completed exchange marked pending")
	}
}

func header(in []HeaderField, name string) string {
	for _, h := range in {
		if h.Name == name {
			return h.Value
		}
	}
	return ""
}
