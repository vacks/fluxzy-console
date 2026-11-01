package httpapi

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"testing"

	"github.com/andybalholm/brotli"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func gzipTestData(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeBodyPreviewGzip(t *testing.T) {
	want := "hello 世界"
	got, decoded := decodeBodyPreview(gzipTestData(t, want), "gzip", "utf-8", 1<<20)
	if !decoded {
		t.Fatal("expected gzip body to be decoded")
	}
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDecodeBodyPreviewGzipAlreadyDecoded(t *testing.T) {
	want := []byte("<html>plain</html>")
	got, decoded := decodeBodyPreview(want, "gzip", "utf-8", 1<<20)
	if decoded {
		t.Fatal("plain body with a gzip header must not be decoded")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDecodeBodyPreviewDeflate(t *testing.T) {
	want := "deflate 内容"
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write([]byte(want)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, decoded := decodeBodyPreview(buf.Bytes(), "deflate", "", 1<<20)
	if !decoded || string(got) != want {
		t.Fatalf("decoded=%v got=%q want=%q", decoded, got, want)
	}
}

func TestDecodeBodyPreviewBrotli(t *testing.T) {
	want := "brotli 内容"
	var buf bytes.Buffer
	w := brotli.NewWriter(&buf)
	if _, err := w.Write([]byte(want)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, decoded := decodeBodyPreview(buf.Bytes(), "br", "", 1<<20)
	if !decoded || string(got) != want {
		t.Fatalf("decoded=%v got=%q want=%q", decoded, got, want)
	}
}

func TestDecodeBodyPreviewGBK(t *testing.T) {
	want := "中文内容"
	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(want))
	if err != nil {
		t.Fatal(err)
	}
	got, decoded := decodeBodyPreview(encoded, "", "gbk", 1<<20)
	if !decoded || string(got) != want {
		t.Fatalf("decoded=%v got=%q want=%q", decoded, got, want)
	}
}

func TestSplitContentType(t *testing.T) {
	base, charset := splitContentType("text/html;charset=UTF-8")
	if base != "text/html" || charset != "UTF-8" {
		t.Fatalf("base=%q charset=%q", base, charset)
	}
}
