package trustca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestCAEnvFiltersSecrets(t *testing.T) {
	t.Setenv("CONSOLE_UNUSED_SECRET", "secret")
	t.Setenv("FLUXZY_ROOT_CERTIFICATE", "/old.pfx")
	env := caEnv("/new.pfx")
	for _, entry := range env {
		if strings.HasPrefix(entry, "CONSOLE_UNUSED_SECRET=") {
			t.Fatal("console secret leaked into CA subprocess env")
		}
		if strings.HasPrefix(entry, "FLUXZY_ROOT_CERTIFICATE=") && entry != "FLUXZY_ROOT_CERTIFICATE=/new.pfx" {
			t.Fatalf("unexpected CA env: %s", entry)
		}
	}
	found := false
	for _, entry := range env {
		if entry == "FLUXZY_ROOT_CERTIFICATE=/new.pfx" {
			found = true
		}
	}
	if !found {
		t.Fatal("new CA path missing")
	}
}

func TestInfoFromCert(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(42),
		Subject:               pkix.Name{CommonName: "Test Root CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	info := infoFromCert(cert, "custom", "/ca/rootca.pfx", 123, 456)
	if info.Source != "custom" || info.CommonName != "Test Root CA" {
		t.Fatalf("info = %+v", info)
	}
	if info.FingerprintSHA256 == "" || info.DaysRemaining < 300 {
		t.Fatalf("info = %+v", info)
	}
}
