package supervisor

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type StartSpec struct {
	ListenAddr             string `json:"listenAddr"`
	Mode                   string `json:"mode"`
	ReversePort            int    `json:"reversePort"`
	MaxUpstreamConnections int    `json:"maxUpstreamConnections"`
	RequestBuffer          int    `json:"requestBuffer"`
	MaxCaptureCount        int64  `json:"maxCaptureCount"`
	ServeH2                bool   `json:"serveH2"`
	ParseUA                bool   `json:"parseUA"`
	NoCertCache            bool   `json:"noCertCache"`
	SkipSSLDecryption      bool   `json:"skipSSLDecryption"`
}

func (s StartSpec) IsReverse() bool {
	return s.Mode == "ReverseSecure" || s.Mode == "ReversePlain"
}

func (s StartSpec) Validate() error {
	switch s.Mode {
	case "Regular", "ReverseSecure", "ReversePlain":
	default:
		return fmt.Errorf("mode 必须是 Regular、ReverseSecure 或 ReversePlain")
	}
	host, port, err := net.SplitHostPort(s.ListenAddr)
	if err != nil {
		return fmt.Errorf("监听地址必须为 host:port: %w", err)
	}
	if strings.ContainsAny(host, " \t\r\n") || strings.ContainsAny(port, " \t\r\n") {
		return fmt.Errorf("监听地址包含非法字符")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("监听端口必须在 1–65535 之间")
	}
	if s.IsReverse() {
		if s.ReversePort < 1 || s.ReversePort > 65535 {
			return fmt.Errorf("反向代理端口必须在 1–65535 之间")
		}
	} else if s.ReversePort != 0 {
		return fmt.Errorf("非反向模式不能设置反向代理端口")
	}
	if s.MaxUpstreamConnections < 1 || s.MaxUpstreamConnections > 1024 {
		return fmt.Errorf("最大上游连接数必须在 1–1024 之间")
	}
	if s.RequestBuffer < 16 || s.RequestBuffer > 524288 {
		return fmt.Errorf("请求缓冲区必须在 16–524288 字节之间")
	}
	if s.MaxCaptureCount < 0 {
		return fmt.Errorf("最大捕获条数不能为负数")
	}
	return nil
}

func (s StartSpec) Args(rulesFile, sessionDir string) []string {
	args := []string{
		"start",
		"-l", s.ListenAddr,
		"--mode", s.Mode,
	}
	if s.IsReverse() {
		args = append(args, "--mode-reverse-port", strconv.Itoa(s.ReversePort))
	}
	args = append(args,
		"-d", sessionDir,
		"-r", rulesFile,
		"--max-upstream-connection", strconv.Itoa(s.MaxUpstreamConnections),
		"--request-buffer", strconv.Itoa(s.RequestBuffer),
	)
	if s.MaxCaptureCount > 0 {
		args = append(args, "-n", strconv.FormatInt(s.MaxCaptureCount, 10))
	}
	if s.ServeH2 {
		args = append(args, "--serve-h2")
	}
	if s.ParseUA {
		args = append(args, "--parse-ua")
	}
	if s.NoCertCache {
		args = append(args, "--no-cert-cache")
	}
	if s.SkipSSLDecryption {
		args = append(args, "--skip-ssl-decryption")
	}
	return args
}

// ProbeAddr turns a bind address into a loopback address suitable for a local
// TCP readiness probe.
func ProbeAddr(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "127.0.0.1:443"
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::", "[::]":
		host = "::1"
	}
	return net.JoinHostPort(host, port)
}
