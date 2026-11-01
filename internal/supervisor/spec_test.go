package supervisor

import "testing"

func TestStartSpecArgsAndValidation(t *testing.T) {
	spec := StartSpec{
		ListenAddr: "0.0.0.0:443", Mode: "ReverseSecure", ReversePort: 443,
		MaxUpstreamConnections: 32, RequestBuffer: 8192, MaxCaptureCount: 100,
		ServeH2: true, ParseUA: true,
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	args := spec.Args("/config/rules.yaml", "/data/sessions/s1")
	required := map[string]bool{
		"start": false, "-l": false, "0.0.0.0:443": false, "--mode": false,
		"ReverseSecure": false, "--mode-reverse-port": false, "443": false,
		"-d": false, "/data/sessions/s1": false, "-r": false,
		"/config/rules.yaml": false, "-n": false, "100": false, "--serve-h2": false,
	}
	for _, a := range args {
		if _, ok := required[a]; ok {
			required[a] = true
		}
	}
	for k, ok := range required {
		if !ok {
			t.Fatalf("missing arg %q in %v", k, args)
		}
	}
}

func TestStartSpecRejectsBadValues(t *testing.T) {
	bad := []StartSpec{
		{ListenAddr: "localhost", Mode: "ReverseSecure", ReversePort: 443, MaxUpstreamConnections: 16, RequestBuffer: 4096},
		{ListenAddr: "0.0.0.0:443", Mode: "Nope", MaxUpstreamConnections: 16, RequestBuffer: 4096},
		{ListenAddr: "0.0.0.0:443", Mode: "ReverseSecure", ReversePort: 0, MaxUpstreamConnections: 16, RequestBuffer: 4096},
		{ListenAddr: "0.0.0.0:443", Mode: "Regular", ReversePort: 443, MaxUpstreamConnections: 16, RequestBuffer: 4096},
		{ListenAddr: "0.0.0.0:443", Mode: "Regular", MaxUpstreamConnections: 0, RequestBuffer: 4096},
		{ListenAddr: "0.0.0.0:443", Mode: "Regular", MaxUpstreamConnections: 16, RequestBuffer: 1},
	}
	for i, s := range bad {
		if err := s.Validate(); err == nil {
			t.Fatalf("case %d unexpectedly valid: %+v", i, s)
		}
	}
}

func TestProbeAddr(t *testing.T) {
	if got := ProbeAddr("0.0.0.0:443"); got != "127.0.0.1:443" {
		t.Fatalf("got %s", got)
	}
	if got := ProbeAddr(":8443"); got != "127.0.0.1:8443" {
		t.Fatalf("got %s", got)
	}
}
