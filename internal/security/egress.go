// Package security 提供出站安全防护，主要防止自定义供应商 base_url 引发的 SSRF/内网探测。
package security

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// EgressGuard 校验上游 base_url 是否允许出站。
type EgressGuard struct {
	allowPrivate bool
	allowHosts   map[string]bool // 白名单主机（精确匹配），即使解析到内网也放行
}

// NewEgressGuard 构建出口守卫。allowPrivate=true 时放行内网（用于自托管场景）。
func NewEgressGuard(allowPrivate bool, allowHosts []string) *EgressGuard {
	m := make(map[string]bool, len(allowHosts))
	for _, h := range allowHosts {
		m[strings.ToLower(h)] = true
	}
	return &EgressGuard{allowPrivate: allowPrivate, allowHosts: m}
}

// Validate 校验 rawURL：必须为 http/https；主机解析后不得为内网/环回/保留地址
// （除非 allowPrivate 或命中白名单）。
func (g *EgressGuard) Validate(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if g.allowPrivate || g.allowHosts[strings.ToLower(host)] {
		return nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve host %q: %w", host, err)
	}
	for _, ip := range ips {
		if isDisallowed(ip) {
			return fmt.Errorf("host %q resolves to disallowed address %s (SSRF guard)", host, ip)
		}
	}
	return nil
}

func isDisallowed(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		isCGNAT(ip)
}

// isCGNAT 判断是否落在运营商级 NAT 段 100.64.0.0/10。
func isCGNAT(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}
