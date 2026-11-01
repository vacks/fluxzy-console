package archive

import (
	"bytes"
	"errors"
	"fmt"
	"time"
	"unicode/utf16"

	"github.com/vmihailenco/msgpack/v5"
)

const nilCode = 0xc0

// A corrupt or torn .mpack file can contain an arbitrary array length prefix.
// Never trust that length when allocating; legitimate exchanges are orders of
// magnitude below this cap.
const maxCollectionElements = 100000

func checkCollectionLen(n int) error {
	if n > maxCollectionElements {
		return fmt.Errorf("array length %d exceeds limit %d", n, maxCollectionElements)
	}
	return nil
}

// DecodeExchange decodes the MessagePack representation of Fluxzy.ExchangeInfo.
// The format uses integer-keyed arrays, UTF-16 code-unit arrays for
// ReadOnlyMemory<char>, and contractless maps for a few nested types.
func DecodeExchange(data []byte) (*Exchange, error) {
	dec := msgpack.NewDecoder(bytes.NewReader(data))
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return nil, fmt.Errorf("exchange array: %w", err)
	}
	if n < 0 {
		return nil, errors.New("exchange is nil")
	}

	ex := &Exchange{}
	for i := 0; i < n; i++ {
		switch i {
		case 0:
			ex.ConnectionID, err = dec.DecodeInt()
		case 1:
			ex.ID, err = dec.DecodeInt()
		case 2:
			ex.Request, err = decodeRequestHeader(dec)
		case 3:
			ex.Response, err = decodeResponseHeader(dec)
		case 4:
			ex.Metrics, err = decodeMetrics(dec)
		case 5:
			ex.Pending, err = dec.DecodeBool()
		case 6:
			ex.HTTPVersion, err = decodeStringLike(dec)
		case 7:
			ex.KnownAuthority, err = decodeStringLike(dec)
		case 8:
			ex.KnownPort, err = dec.DecodeInt()
		case 9:
			ex.Secure, err = dec.DecodeBool()
		case 10:
			ex.EgressIP, err = decodeOptionalString(dec)
		case 11:
			ex.Comment, err = decodeOptionalString(dec)
		case 12:
			ex.Tags, err = decodeTags(dec)
		case 13:
			ex.IsWebSocket, err = dec.DecodeBool()
		case 14:
			ex.WebSocketMessages, err = decodeWsMessages(dec)
		case 15:
			ex.Agent, err = decodeAgent(dec)
		case 16:
			ex.ClientErrors, err = decodeClientErrors(dec)
		case 17:
			ex.ProcessInfo, err = decodeProcessInfo(dec)
		case 18:
			ex.RequestTrailers, err = decodeHeaders(dec)
		case 19:
			ex.ResponseTrailers, err = decodeHeaders(dec)
		default:
			err = dec.Skip()
		}
		if err != nil {
			return nil, fmt.Errorf("exchange field %d: %w", i, err)
		}
	}

	if ex.ID <= 0 {
		return nil, errors.New("exchange id is missing")
	}
	return ex, nil
}

func decodeRequestHeader(dec *msgpack.Decoder) (RequestHeader, error) {
	var h RequestHeader
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return h, err
	}
	if n < 0 {
		return h, nil
	}
	for i := 0; i < n; i++ {
		switch i {
		case 0:
			h.Method, err = decodeCharMemory(dec)
		case 1:
			h.Scheme, err = decodeCharMemory(dec)
		case 2:
			h.Path, err = decodeCharMemory(dec)
		case 3:
			h.Authority, err = decodeCharMemory(dec)
		case 4:
			h.Headers, err = decodeHeaders(dec)
		default:
			err = dec.Skip()
		}
		if err != nil {
			return h, err
		}
	}
	return h, nil
}

func decodeResponseHeader(dec *msgpack.Decoder) (*ResponseHeader, error) {
	nilValue, err := isNil(dec)
	if err != nil {
		return nil, err
	}
	if nilValue {
		return nil, dec.DecodeNil()
	}
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, nil
	}
	h := &ResponseHeader{}
	for i := 0; i < n; i++ {
		switch i {
		case 0:
			h.StatusCode, err = dec.DecodeInt()
		case 1:
			h.Headers, err = decodeHeaders(dec)
		default:
			err = dec.Skip()
		}
		if err != nil {
			return nil, err
		}
	}
	return h, nil
}

func decodeHeaders(dec *msgpack.Decoder) ([]HeaderField, error) {
	nilValue, err := isNil(dec)
	if err != nil {
		return nil, err
	}
	if nilValue {
		return nil, dec.DecodeNil()
	}
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, nil
	}
	if err := checkCollectionLen(n); err != nil {
		return nil, err
	}
	out := make([]HeaderField, 0, n)
	for i := 0; i < n; i++ {
		hn, err := dec.DecodeArrayLen()
		if err != nil {
			return nil, err
		}
		if hn < 0 {
			continue
		}
		var h HeaderField
		for j := 0; j < hn; j++ {
			switch j {
			case 0:
				h.Name, err = decodeCharMemory(dec)
			case 1:
				h.Value, err = decodeCharMemory(dec)
			case 2:
				h.Forwarded, err = dec.DecodeBool()
			default:
				err = dec.Skip()
			}
			if err != nil {
				return nil, err
			}
		}
		out = append(out, h)
	}
	return out, nil
}

func decodeMetrics(dec *msgpack.Decoder) (Metrics, error) {
	var m Metrics
	nilValue, err := isNil(dec)
	if err != nil {
		return m, err
	}
	if nilValue {
		return m, dec.DecodeNil()
	}
	n, err := dec.DecodeMapLen()
	if err != nil {
		return m, err
	}
	if n < 0 {
		return m, nil
	}
	for i := 0; i < n; i++ {
		key, err := dec.DecodeString()
		if err != nil {
			return m, err
		}
		switch key {
		case "ReusingConnection":
			m.ReusingConnection, err = dec.DecodeBool()
		case "ReceivedFromProxy":
			m.ReceivedFromProxy, err = decodeTime(dec)
		case "RetrievingPool":
			m.RetrievingPool, err = decodeTime(dec)
		case "RequestHeaderSending":
			m.RequestHeaderSending, err = decodeTime(dec)
		case "RequestHeaderSent":
			m.RequestHeaderSent, err = decodeTime(dec)
		case "RequestBodySent":
			m.RequestBodySent, err = decodeTime(dec)
		case "ResponseHeaderStart":
			m.ResponseHeaderStart, err = decodeTime(dec)
		case "ResponseHeaderEnd":
			m.ResponseHeaderEnd, err = decodeTime(dec)
		case "ResponseBodyStart":
			m.ResponseBodyStart, err = decodeTime(dec)
		case "ResponseBodyEnd":
			m.ResponseBodyEnd, err = decodeTime(dec)
		case "RemoteClosed":
			m.RemoteClosed, err = decodeTime(dec)
		case "CreateCertStart":
			m.CreateCertStart, err = decodeTime(dec)
		case "CreateCertEnd":
			m.CreateCertEnd, err = decodeTime(dec)
		case "ErrorInstant":
			m.ErrorInstant, err = decodeTime(dec)
		case "TotalSent":
			m.TotalSent, err = dec.DecodeInt64()
		case "TotalReceived":
			m.TotalReceived, err = dec.DecodeInt64()
		case "RequestHeaderLength":
			m.RequestHeaderLength, err = dec.DecodeInt()
		case "ResponseHeaderLength":
			m.ResponseHeaderLength, err = dec.DecodeInt()
		case "DownStreamClientPort":
			m.DownStreamClientPort, err = dec.DecodeInt()
		case "DownStreamLocalPort":
			m.DownStreamLocalPort, err = dec.DecodeInt()
		case "DownStreamClientAddress":
			m.DownStreamClientAddress, err = decodeStringLike(dec)
		case "DownStreamLocalAddress":
			m.DownStreamLocalAddress, err = decodeStringLike(dec)
		default:
			err = dec.Skip()
		}
		if err != nil {
			return m, fmt.Errorf("metric %s: %w", key, err)
		}
	}
	return m, nil
}

func decodeTags(dec *msgpack.Decoder) ([]string, error) {
	nilValue, err := isNil(dec)
	if err != nil {
		return nil, err
	}
	if nilValue {
		return nil, dec.DecodeNil()
	}
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, nil
	}
	if err := checkCollectionLen(n); err != nil {
		return nil, err
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		tn, err := dec.DecodeArrayLen()
		if err != nil {
			return nil, err
		}
		if tn < 0 {
			continue
		}
		var value string
		for j := 0; j < tn; j++ {
			switch j {
			case 0:
				_, err = decodeGuid(dec)
			case 1:
				value, err = decodeStringLike(dec)
			default:
				err = dec.Skip()
			}
			if err != nil {
				return nil, err
			}
		}
		if value != "" {
			out = append(out, value)
		}
	}
	return out, nil
}

func decodeAgent(dec *msgpack.Decoder) (*Agent, error) {
	nilValue, err := isNil(dec)
	if err != nil {
		return nil, err
	}
	if nilValue {
		return nil, dec.DecodeNil()
	}
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, nil
	}
	a := &Agent{}
	for i := 0; i < n; i++ {
		switch i {
		case 0:
			a.ID, err = dec.DecodeInt()
		case 1:
			a.FriendlyName, err = decodeStringLike(dec)
		default:
			err = dec.Skip()
		}
		if err != nil {
			return nil, err
		}
	}
	return a, nil
}

func decodeClientErrors(dec *msgpack.Decoder) ([]ClientError, error) {
	nilValue, err := isNil(dec)
	if err != nil {
		return nil, err
	}
	if nilValue {
		return nil, dec.DecodeNil()
	}
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, nil
	}
	if err := checkCollectionLen(n); err != nil {
		return nil, err
	}
	out := make([]ClientError, 0, n)
	for i := 0; i < n; i++ {
		en, err := dec.DecodeArrayLen()
		if err != nil {
			return nil, err
		}
		if en < 0 {
			continue
		}
		var e ClientError
		for j := 0; j < en; j++ {
			switch j {
			case 0:
				e.ErrorCode, err = dec.DecodeInt()
			case 1:
				e.Message, err = decodeStringLike(dec)
			case 2:
				e.ExceptionMessage, err = decodeOptionalString(dec)
			case 3:
				e.NetworkErrorCode, err = decodeOptionalString(dec)
			default:
				err = dec.Skip()
			}
			if err != nil {
				return nil, err
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func decodeProcessInfo(dec *msgpack.Decoder) (*ProcessInfo, error) {
	nilValue, err := isNil(dec)
	if err != nil {
		return nil, err
	}
	if nilValue {
		return nil, dec.DecodeNil()
	}
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, nil
	}
	p := &ProcessInfo{}
	for i := 0; i < n; i++ {
		switch i {
		case 0:
			p.ProcessID, err = dec.DecodeInt()
		case 1:
			p.ProcessPath, err = decodeOptionalString(dec)
		case 2:
			p.ProcessArguments, err = decodeOptionalString(dec)
		default:
			err = dec.Skip()
		}
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}

func decodeWsMessages(dec *msgpack.Decoder) ([]WsMessage, error) {
	nilValue, err := isNil(dec)
	if err != nil {
		return nil, err
	}
	if nilValue {
		return nil, dec.DecodeNil()
	}
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, nil
	}
	if err := checkCollectionLen(n); err != nil {
		return nil, err
	}
	out := make([]WsMessage, 0, n)
	for i := 0; i < n; i++ {
		mn, err := dec.DecodeArrayLen()
		if err != nil {
			return nil, err
		}
		if mn < 0 {
			continue
		}
		var m WsMessage
		for j := 0; j < mn; j++ {
			switch j {
			case 0:
				m.ID, err = dec.DecodeInt()
			case 1:
				m.Direction, err = dec.DecodeInt()
			case 2:
				m.OpCode, err = dec.DecodeInt()
			case 3:
				m.Length, err = dec.DecodeInt64()
			case 4:
				m.WrittenLength, err = dec.DecodeInt64()
			case 5:
				m.Data, err = decodeBytesOrNil(dec)
			case 6:
				err = dec.Skip()
			case 7:
				m.DataString, err = decodeOptionalString(dec)
			case 8:
				m.MessageStart, err = decodeTime(dec)
			case 9:
				m.MessageEnd, err = decodeTime(dec)
			case 10:
				m.FrameCount, err = dec.DecodeInt()
			default:
				err = dec.Skip()
			}
			if err != nil {
				return nil, err
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// DecodeConnection decodes ConnectionInfo. It is used for the connection tab in
// the exchange detail view.
func DecodeConnection(data []byte) (*Connection, error) {
	dec := msgpack.NewDecoder(bytes.NewReader(data))
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, errors.New("connection is nil")
	}
	c := &Connection{}
	for i := 0; i < n; i++ {
		switch i {
		case 0:
			c.ID, err = dec.DecodeInt()
		case 1:
			c.HTTPVersion, err = decodeOptionalString(dec)
		case 2:
			c.Authority, err = decodeAuthority(dec)
		case 3:
			err = dec.Skip()
		case 4:
			c.RequestProcessed, err = dec.DecodeInt()
		case 5:
			c.DnsSolveStart, err = decodeTime(dec)
		case 6:
			c.DnsSolveEnd, err = decodeTime(dec)
		case 7:
			c.TcpConnectionOpening, err = decodeTime(dec)
		case 8:
			c.TcpConnectionOpened, err = decodeTime(dec)
		case 9:
			c.SslNegotiationStart, err = decodeTime(dec)
		case 10:
			c.SslNegotiationEnd, err = decodeTime(dec)
		case 11:
			c.LocalPort, err = dec.DecodeInt()
		case 12:
			c.LocalAddress, err = decodeOptionalString(dec)
		case 13:
			c.RemoteAddress, err = decodeOptionalString(dec)
		case 14:
			c.ProxyConnectStart, err = decodeTime(dec)
		case 15:
			c.ProxyConnectEnd, err = decodeTime(dec)
		default:
			err = dec.Skip()
		}
		if err != nil {
			return nil, fmt.Errorf("connection field %d: %w", i, err)
		}
	}
	return c, nil
}

func decodeAuthority(dec *msgpack.Decoder) (Authority, error) {
	var a Authority
	nilValue, err := isNil(dec)
	if err != nil {
		return a, err
	}
	if nilValue {
		return a, dec.DecodeNil()
	}
	n, err := dec.DecodeMapLen()
	if err != nil {
		return a, err
	}
	if n < 0 {
		return a, nil
	}
	for i := 0; i < n; i++ {
		key, err := dec.DecodeString()
		if err != nil {
			return a, err
		}
		switch key {
		case "HostName":
			a.HostName, err = decodeStringLike(dec)
		case "Port":
			a.Port, err = dec.DecodeInt()
		case "Secure":
			a.Secure, err = dec.DecodeBool()
		default:
			err = dec.Skip()
		}
		if err != nil {
			return a, err
		}
	}
	return a, nil
}

func decodeCharMemory(dec *msgpack.Decoder) (string, error) {
	code, err := dec.PeekCode()
	if err != nil {
		return "", err
	}
	if code == nilCode {
		return "", dec.DecodeNil()
	}
	if isStringCode(code) {
		return dec.DecodeString()
	}
	if isBinCode(code) {
		b, err := dec.DecodeBytes()
		return string(b), err
	}
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return "", err
	}
	if n < 0 {
		return "", nil
	}
	if err := checkCollectionLen(n); err != nil {
		return "", err
	}
	u := make([]uint16, n)
	for i := 0; i < n; i++ {
		v, err := dec.DecodeUint16()
		if err != nil {
			return "", err
		}
		u[i] = v
	}
	return string(utf16.Decode(u)), nil
}

func decodeStringLike(dec *msgpack.Decoder) (string, error) {
	code, err := dec.PeekCode()
	if err != nil {
		return "", err
	}
	if code == nilCode {
		return "", dec.DecodeNil()
	}
	if isStringCode(code) {
		return dec.DecodeString()
	}
	if isBinCode(code) {
		b, err := dec.DecodeBytes()
		return string(b), err
	}
	return decodeCharMemory(dec)
}

func decodeOptionalString(dec *msgpack.Decoder) (string, error) {
	return decodeStringLike(dec)
}

func decodeGuid(dec *msgpack.Decoder) (string, error) {
	code, err := dec.PeekCode()
	if err != nil {
		return "", err
	}
	if code == nilCode {
		return "", dec.DecodeNil()
	}
	if isBinCode(code) {
		b, err := dec.DecodeBytes()
		if err != nil {
			return "", err
		}
		if len(b) == 16 {
			return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
		}
		return fmt.Sprintf("%x", b), nil
	}
	return decodeStringLike(dec)
}

func decodeBytesOrNil(dec *msgpack.Decoder) ([]byte, error) {
	code, err := dec.PeekCode()
	if err != nil {
		return nil, err
	}
	if code == nilCode {
		return nil, dec.DecodeNil()
	}
	if isBinCode(code) {
		return dec.DecodeBytes()
	}
	var s string
	s, err = decodeStringLike(dec)
	return []byte(s), err
}

func decodeTime(dec *msgpack.Decoder) (time.Time, error) {
	code, err := dec.PeekCode()
	if err != nil {
		return time.Time{}, err
	}
	if code == nilCode {
		return time.Time{}, dec.DecodeNil()
	}
	if isStringCode(code) {
		s, err := dec.DecodeString()
		if err != nil {
			return time.Time{}, err
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("unsupported datetime %q", s)
	}
	return dec.DecodeTime()
}

func isNil(dec *msgpack.Decoder) (bool, error) {
	code, err := dec.PeekCode()
	if err != nil {
		return false, err
	}
	return code == nilCode, nil
}

func isStringCode(code byte) bool {
	return (code >= 0xa0 && code <= 0xbf) || code == 0xd9 || code == 0xda || code == 0xdb
}

func isBinCode(code byte) bool {
	return code == 0xc4 || code == 0xc5 || code == 0xc6
}
