package parser

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ToURI converts a Profile back into its native link format.
func (p *Profile) ToURI() string {
	switch p.Protocol {
	case "vmess":
		return p.toVMessURI()
	case "shadowsocks":
		return p.toShadowsocksURI()
	default:
		// VLESS, Trojan, Hysteria2, WireGuard, Socks, HTTP share a common URI structure
		return p.toGenericURI()
	}
}

func (p *Profile) toVMessURI() string {
	v := vmessJSON{
		V:    "2",
		Ps:   p.Remarks,
		Add:  p.Address,
		Port: p.Port,
		Id:   p.Password,
		Scy:  p.Method,
		Net:  p.Network,
		Type: p.HeaderType,
		Host: p.Host,
		Path: p.Path,
		Tls:  p.Security,
		Sni:  p.SNI,
		Alpn: strings.Join(p.ALPN, ","),
		Fp:   p.Fingerprint,
	}
	
	if p.Network == "grpc" {
		v.Type = p.Mode
		v.Path = p.ServiceName
	} else if p.Network == "kcp" {
		v.Path = p.Seed
	}

	b, _ := json.Marshal(v)
	return "vmess://" + base64.StdEncoding.EncodeToString(b)
}

func (p *Profile) toShadowsocksURI() string {
	userInfo := fmt.Sprintf("%s:%s", p.Method, p.Password)
	
	// Use SIP002 (safe for special chars)
	safeUser := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(userInfo))
	
	u := url.URL{
		Scheme:   "ss",
		User:     url.User(safeUser),
		Host:     fmt.Sprintf("%s:%d", p.Address, p.Port),
		Fragment: p.Remarks,
	}
	
	// Add Plugin params if HTTP obfs (simplified)
	if p.HeaderType == "http" {
		plugin := fmt.Sprintf("obfs-local;obfs=http;obfs-host=%s", p.Host)
		if p.Path != "" {
			plugin += fmt.Sprintf(";path=%s", p.Path)
		}
		q := u.Query()
		q.Set("plugin", plugin)
		u.RawQuery = q.Encode()
	}

	return u.String()
}

func (p *Profile) toGenericURI() string {
	u := url.URL{
		Scheme: p.Protocol,
		Host:   fmt.Sprintf("%s:%d", p.Address, p.Port),
		Fragment: p.Remarks,
	}
	
	if p.Username != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	} else if p.Password != "" {
		u.User = url.User(p.Password)
	}

	q := u.Query()
	
	// Common Transport
	if p.Network != "" && p.Network != "tcp" {
		q.Set("type", p.Network)
	}
	if p.Security != "" {
		q.Set("security", p.Security)
	}
	if p.SNI != "" {
		q.Set("sni", p.SNI)
	}
	if p.Fingerprint != "" {
		q.Set("fp", p.Fingerprint)
	}
	if p.Host != "" {
		q.Set("host", p.Host)
	}
	if p.Path != "" {
		q.Set("path", p.Path)
	}
	if p.HeaderType != "" && p.HeaderType != "none" {
		q.Set("headerType", p.HeaderType)
	}
	if p.ServiceName != "" {
		q.Set("serviceName", p.ServiceName)
	}
	if p.Mode != "" {
		q.Set("mode", p.Mode)
	}
	if len(p.ALPN) > 0 {
		q.Set("alpn", strings.Join(p.ALPN, ","))
	}
	if p.Insecure {
		q.Set("allowInsecure", "1")
	}

	// Reality
	if p.Pbk != "" {
		q.Set("pbk", p.Pbk)
	}
	if p.Sid != "" {
		q.Set("sid", p.Sid)
	}
	if p.SpiderX != "" {
		q.Set("spx", p.SpiderX)
	}
	if p.Flow != "" {
		q.Set("flow", p.Flow)
	}

	// WireGuard
	if p.Protocol == "wireguard" {
		q.Set("publickey", p.PublicKey)
		if p.PreSharedKey != "" {
			q.Set("presharedkey", p.PreSharedKey)
		}
		if p.LocalAddress != "" {
			q.Set("address", p.LocalAddress)
		}
		if p.MTU > 0 {
			q.Set("mtu", fmt.Sprintf("%d", p.MTU))
		}
		if len(p.Reserved) > 0 {
			parts := make([]string, len(p.Reserved))
			for i, b := range p.Reserved {
				parts[i] = fmt.Sprintf("%d", b)
			}
			q.Set("reserved", strings.Join(parts, ","))
		}
	}
	
	// Hysteria2
	if p.Protocol == "hysteria2" {
		if p.ObfsPassword != "" {
			q.Set("obfs-password", p.ObfsPassword)
		}
		if p.PortHopping != "" {
			q.Set("mport", p.PortHopping)
		}
	}

	u.RawQuery = q.Encode()
	return u.String()
}
