package supervisor

import (
	"io"
	"strings"
	"sync"
	"time"
)

type LogLine struct {
	Seq    int64  `json:"seq"`
	At     int64  `json:"at"`
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

type LogBuffer struct {
	mu      sync.Mutex
	lines   []LogLine
	start   int
	max     int
	seq     int64
	streams map[string]*lineWriter
	out     io.Writer
	errOut  io.Writer
}

func NewLogBuffer(max int, out, errOut io.Writer) *LogBuffer {
	if max <= 0 {
		max = 5000
	}
	b := &LogBuffer{max: max, out: out, errOut: errOut, streams: make(map[string]*lineWriter)}
	b.streams["stdout"] = &lineWriter{buf: b, stream: "stdout", out: out}
	b.streams["stderr"] = &lineWriter{buf: b, stream: "stderr", out: errOut}
	return b
}

func (b *LogBuffer) Stdout() io.Writer { return b.streams["stdout"] }
func (b *LogBuffer) Stderr() io.Writer { return b.streams["stderr"] }

func (b *LogBuffer) append(stream, text string) {
	b.mu.Lock()
	b.seq++
	line := LogLine{Seq: b.seq, At: time.Now().UnixMilli(), Stream: stream, Text: text}
	if len(b.lines) < b.max {
		b.lines = append(b.lines, line)
	} else {
		b.lines[b.start] = line
		b.start = (b.start + 1) % b.max
	}
	b.mu.Unlock()
}

// forEachLocked visits buffered lines oldest-first. Callers must hold b.mu.
func (b *LogBuffer) forEachLocked(fn func(LogLine) bool) {
	if len(b.lines) < b.max {
		for _, line := range b.lines {
			if !fn(line) {
				return
			}
		}
		return
	}
	for i := 0; i < b.max; i++ {
		if !fn(b.lines[(b.start+i)%b.max]) {
			return
		}
	}
}

func (b *LogBuffer) LastSeq() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}

// ReadySince reports whether fluxzy printed its startup banner after seq. The
// process itself writing the banner is more reliable than a TCP probe, which
// can accidentally hit another process already bound to the same port.
func (b *LogBuffer) ReadySince(seq int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	found := false
	b.forEachLocked(func(line LogLine) bool {
		if line.Seq > seq &&
			(strings.Contains(line.Text, "Ready to process connections") ||
				strings.Contains(line.Text, "Listen on ")) {
			found = true
			return false
		}
		return true
	})
	return found
}

// Tail returns the last n lines. If since > 0, it returns lines with a sequence
// greater than since instead.
func (b *LogBuffer) Tail(since int64, n int) []LogLine {
	if n <= 0 || n > b.max {
		n = 200
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if since > 0 {
		out := make([]LogLine, 0, n)
		b.forEachLocked(func(line LogLine) bool {
			if line.Seq > since {
				out = append(out, line)
			}
			return len(out) < n
		})
		return out
	}
	total := len(b.lines)
	skip := total - n
	if skip < 0 {
		skip = 0
	}
	seen := 0
	out := make([]LogLine, 0, total-skip)
	b.forEachLocked(func(line LogLine) bool {
		if seen >= skip {
			out = append(out, line)
		}
		seen++
		return len(out) < n
	})
	return out
}

type lineWriter struct {
	mu     sync.Mutex
	buf    *LogBuffer
	stream string
	out    io.Writer
	part   strings.Builder
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.out != nil {
		_, _ = w.out.Write(p)
	}
	s := string(p)
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			w.part.WriteString(s)
			break
		}
		w.part.WriteString(s[:i])
		w.buf.append(w.stream, strings.TrimSuffix(w.part.String(), "\r"))
		w.part.Reset()
		s = s[i+1:]
	}
	return len(p), nil
}
