package archive

import (
	"strings"
	"time"
)

// Exchange mirrors the user-facing subset of Fluxzy.ExchangeInfo. The wire
// format is MessagePack and is decoded in decode.go.
type Exchange struct {
	ConnectionID      int
	ID                int
	Request           RequestHeader
	Response          *ResponseHeader
	Metrics           Metrics
	Pending           bool
	HTTPVersion       string
	KnownAuthority    string
	KnownPort         int
	Secure            bool
	EgressIP          string
	Comment           string
	Tags              []string
	IsWebSocket       bool
	WebSocketMessages []WsMessage
	Agent             *Agent
	ClientErrors      []ClientError
	ProcessInfo       *ProcessInfo
	RequestTrailers   []HeaderField
	ResponseTrailers  []HeaderField
}

type RequestHeader struct {
	Method    string
	Scheme    string
	Path      string
	Authority string
	Headers   []HeaderField
}

type ResponseHeader struct {
	StatusCode int
	Headers    []HeaderField
}

type HeaderField struct {
	Name      string
	Value     string
	Forwarded bool
}

type Metrics struct {
	ReusingConnection       bool
	ReceivedFromProxy       time.Time
	RetrievingPool          time.Time
	RequestHeaderSending    time.Time
	RequestHeaderSent       time.Time
	RequestBodySent         time.Time
	ResponseHeaderStart     time.Time
	ResponseHeaderEnd       time.Time
	ResponseBodyStart       time.Time
	ResponseBodyEnd         time.Time
	RemoteClosed            time.Time
	CreateCertStart         time.Time
	CreateCertEnd           time.Time
	ErrorInstant            time.Time
	TotalSent               int64
	TotalReceived           int64
	RequestHeaderLength     int
	ResponseHeaderLength    int
	DownStreamClientPort    int
	DownStreamLocalPort     int
	DownStreamClientAddress string
	DownStreamLocalAddress  string
}

type Agent struct {
	ID           int
	FriendlyName string
}

type ClientError struct {
	ErrorCode        int
	Message          string
	ExceptionMessage string
	NetworkErrorCode string
}

type ProcessInfo struct {
	ProcessID        int
	ProcessPath      string
	ProcessArguments string
}

type WsMessage struct {
	ID            int
	Direction     int
	OpCode        int
	Length        int64
	WrittenLength int64
	Data          []byte
	DataString    string
	MessageStart  time.Time
	MessageEnd    time.Time
	FrameCount    int
}

type Connection struct {
	ID                   int
	HTTPVersion          string
	Authority            Authority
	RequestProcessed     int
	DnsSolveStart        time.Time
	DnsSolveEnd          time.Time
	TcpConnectionOpening time.Time
	TcpConnectionOpened  time.Time
	SslNegotiationStart  time.Time
	SslNegotiationEnd    time.Time
	LocalPort            int
	LocalAddress         string
	RemoteAddress        string
	ProxyConnectStart    time.Time
	ProxyConnectEnd      time.Time
}

type Authority struct {
	HostName string
	Port     int
	Secure   bool
}

func (e *Exchange) FullURL() string {
	p := e.Request.Path
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	scheme := e.Request.Scheme
	if scheme == "" {
		if e.Secure {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := e.Request.Authority
	if host == "" {
		host = e.KnownAuthority
	}
	if host == "" {
		return p
	}
	return scheme + "://" + host + p
}

func (e *Exchange) Host() string {
	if e.Request.Authority != "" {
		h := e.Request.Authority
		if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i+1:], "]") {
			return h[:i]
		}
		return h
	}
	return e.KnownAuthority
}

func (e *Exchange) PathOnly() string {
	p := e.Request.Path
	if i := strings.Index(p, "://"); i >= 0 {
		if j := strings.Index(p[i+3:], "/"); j >= 0 {
			return p[i+3+j:]
		}
		return "/"
	}
	return p
}

func (e *Exchange) StatusCode() int {
	if e.Response == nil {
		return 0
	}
	return e.Response.StatusCode
}

func (e *Exchange) ContentType() string {
	if e.Response != nil {
		for _, h := range e.Response.Headers {
			if strings.EqualFold(h.Name, "content-type") {
				return normalizeContentType(h.Value)
			}
		}
	}
	return ""
}

func normalizeContentType(v string) string {
	if i := strings.Index(v, ";"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(strings.ToLower(v))
}

func (m Metrics) Duration() time.Duration {
	start := m.ReceivedFromProxy
	if start.IsZero() {
		return 0
	}
	end := m.ResponseBodyEnd
	if end.IsZero() {
		end = m.ResponseHeaderEnd
	}
	if end.IsZero() {
		end = m.ResponseHeaderStart
	}
	if end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start)
}

func (m Metrics) StartedAt() time.Time {
	if !m.ReceivedFromProxy.IsZero() {
		return m.ReceivedFromProxy
	}
	if !m.RetrievingPool.IsZero() {
		return m.RetrievingPool
	}
	return m.RequestHeaderSending
}
