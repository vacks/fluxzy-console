package httpapi

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// decodeBodyPreview turns the stored body into UTF-8 text for the preview
// endpoint. Fluxzy may store the raw upstream bytes (still gzip/deflate/br
// compressed) while keeping Content-Encoding in the captured headers, and it
// may also store an already decoded body. gzip/deflate are only decoded when
// their stream signature matches; Brotli is attempted and falls back to the
// original bytes when decoding fails.
func decodeBodyPreview(data []byte, contentEncoding, charset string, limit int64) ([]byte, bool) {
	decoded := false
	if enc := outerContentEncoding(contentEncoding); enc != "" {
		if out, ok := decodeContentEncoding(data, enc, limit); ok {
			data = out
			decoded = true
		}
	}
	if out, ok := decodeCharset(data, charset); ok {
		data = out
		decoded = true
	}
	return data, decoded
}

func outerContentEncoding(value string) string {
	parts := strings.Split(value, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		enc := strings.ToLower(strings.TrimSpace(parts[i]))
		if enc != "" && enc != "identity" {
			return enc
		}
	}
	return ""
}

func decodeContentEncoding(data []byte, enc string, limit int64) ([]byte, bool) {
	switch enc {
	case "gzip", "x-gzip":
		if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
			return data, false
		}
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return data, false
		}
		defer r.Close()
		out, err := readLimited(r, limit)
		if err != nil && len(out) == 0 {
			return data, false
		}
		return out, true
	case "deflate":
		if len(data) >= 2 && data[0]&0x0f == 8 && (uint16(data[0])<<8|uint16(data[1]))%31 == 0 {
			if r, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
				defer r.Close()
				if out, err := readLimited(r, limit); err == nil || len(out) > 0 {
					return out, true
				}
			}
		}
		r := flate.NewReader(bytes.NewReader(data))
		defer r.Close()
		out, err := readLimited(r, limit)
		if err != nil || len(out) == 0 {
			return data, false
		}
		return out, true
	case "br":
		out, err := readLimited(brotli.NewReader(bytes.NewReader(data)), limit)
		if err != nil || len(out) == 0 {
			return data, false
		}
		return out, true
	default:
		return data, false
	}
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = 8 << 20
	}
	return io.ReadAll(io.LimitReader(r, limit+1))
}

func decodeCharset(data []byte, label string) ([]byte, bool) {
	label = strings.ToLower(strings.Trim(strings.TrimSpace(label), `"'`))
	var enc encoding.Encoding
	switch label {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return data, false
	case "gbk", "gb2312":
		enc = simplifiedchinese.GBK
	case "gb18030":
		enc = simplifiedchinese.GB18030
	case "big5":
		enc = traditionalchinese.Big5
	case "iso-8859-1", "latin1":
		enc = charmap.ISO8859_1
	default:
		return data, false
	}
	out, err := enc.NewDecoder().Bytes(data)
	if err != nil {
		return data, false
	}
	return out, true
}

func splitContentType(value string) (base, charset string) {
	parts := strings.Split(value, ";")
	base = strings.ToLower(strings.TrimSpace(parts[0]))
	for _, part := range parts[1:] {
		key, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "charset") {
			charset = strings.Trim(strings.TrimSpace(val), `"'`)
		}
	}
	return base, charset
}
