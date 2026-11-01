package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/vacks/fluxzy-console/internal/live"
)

type wsHello struct {
	ActiveSessionID string `json:"activeSessionId"`
	Online          bool   `json:"online"`
	Indexing        bool   `json:"indexing"`
	Stats           any    `json:"stats"`
	MaxEventsPerSec int    `json:"maxEventsPerSec"`
}

type clientMessage struct {
	Type   string `json:"type"`
	LastID int64  `json:"lastId"`
}

func (h *Handlers) liveWS(w http.ResponseWriter, r *http.Request) {
	clearWriteDeadline(w)
	if !h.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_not_allowed", "WebSocket Origin 不被允许", nil)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // origin is checked above
		CompressionMode:    websocket.CompressionContextTakeover,
	})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sub := h.deps.Hub.Subscribe(h.deps.Cfg.MaxLiveEventsPerSec)
	if sub == nil {
		_ = conn.Close(websocket.StatusTryAgainLater, "too many live connections")
		return
	}
	defer h.deps.Hub.Unsubscribe(sub)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Ping(pingCtx)
				pingCancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	control := make(chan clientMessage, 8)
	go func() {
		defer cancel()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var msg clientMessage
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			select {
			case control <- msg:
			default:
			}
		}
	}()

	st := h.deps.Capture.Status()
	hello := wsHello{
		ActiveSessionID: st.ActiveSessionID,
		Online:          st.Online,
		Indexing:        st.Indexing,
		Stats:           st.Stats,
		MaxEventsPerSec: h.deps.Cfg.MaxLiveEventsPerSec,
	}
	if err := writeWS(ctx, conn, live.Event{Type: "hello", Data: hello}); err != nil {
		return
	}

	statsTicker := time.NewTicker(time.Second)
	defer statsTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-sub.Events:
			if err := writeWS(ctx, conn, ev); err != nil {
				return
			}
		case msg := <-control:
			switch msg.Type {
			case "pause":
				sub.Pause()
				if err := writeWS(ctx, conn, live.Event{Type: "paused", Data: map[string]any{
					"paused": true, "sent": sub.Sent(), "dropped": sub.Dropped(),
				}}); err != nil {
					return
				}
			case "resume":
				sub.Resume()
				if err := writeWS(ctx, conn, live.Event{Type: "resync", Data: map[string]any{
					"activeSessionId": h.deps.Capture.ActiveSessionID(),
					"lastId":          msg.LastID,
				}}); err != nil {
					return
				}
			case "ping":
				if err := writeWS(ctx, conn, live.Event{Type: "pong"}); err != nil {
					return
				}
			}
		case <-statsTicker.C:
			st := h.deps.Capture.Status()
			ev := live.Event{Type: "stats", Data: map[string]any{
				"stats":           st.Stats,
				"online":          st.Online,
				"activeSessionId": st.ActiveSessionID,
				"indexing":        st.Indexing,
				"subscriber": map[string]any{
					"paused":  sub.Paused(),
					"sent":    sub.Sent(),
					"dropped": sub.Dropped(),
				},
			}}
			if err := writeWS(ctx, conn, ev); err != nil {
				return
			}
		}
	}
}

func writeWS(ctx context.Context, conn *websocket.Conn, ev live.Event) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, b)
}

func (h *Handlers) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range h.deps.Cfg.AllowedOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if h.deps.Cfg.TrustProxyHeaders {
		if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
			host = xfh
		}
	}
	return strings.EqualFold(u.Host, host)
}
