package security

import "testing"

func TestValidate_BlocksPrivateAndLoopback(t *testing.T) {
	g := NewEgressGuard(false, nil)
	for _, raw := range []string{
		"http://127.0.0.1/v1",
		"http://10.0.0.5:8000/v1",
		"http://192.168.1.1/v1",
		"http://169.254.0.1/v1",
		"http://100.64.0.1/v1",
	} {
		if err := g.Validate(raw); err == nil {
			t.Errorf("Validate(%q) = nil, want SSRF error", raw)
		}
	}
}

func TestValidate_AllowsPublic(t *testing.T) {
	g := NewEgressGuard(false, nil)
	// IP 字面量不触发 DNS；8.8.8.8 为公网地址。
	if err := g.Validate("https://8.8.8.8/v1"); err != nil {
		t.Errorf("public IP should pass: %v", err)
	}
}

func TestValidate_Scheme(t *testing.T) {
	g := NewEgressGuard(true, nil)
	if err := g.Validate("ftp://example.com"); err == nil {
		t.Error("non-http scheme should be rejected")
	}
}

func TestValidate_AllowPrivate(t *testing.T) {
	g := NewEgressGuard(true, nil)
	if err := g.Validate("http://10.0.0.5:8000/v1"); err != nil {
		t.Errorf("allowPrivate should permit internal address: %v", err)
	}
}

func TestValidate_Allowlist(t *testing.T) {
	g := NewEgressGuard(false, []string{"internal.local"})
	if err := g.Validate("http://internal.local/v1"); err != nil {
		t.Errorf("allowlisted host should pass without DNS check: %v", err)
	}
}
