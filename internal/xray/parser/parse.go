package parser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func Parse(raw string) (*Profile, error) {
	raw = FixIllegalUrl(raw)
	parts := strings.SplitN(raw, "://", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid uri format")
	}

	scheme := strings.ToLower(parts[0])
	switch scheme {
	case "vmess":
		return parseVMess(raw)
	case "vless":
		return parseVLESS(raw)
	case "trojan":
		return parseTrojan(raw)
	case "ss", "shadowsocks":
		return parseShadowsocks(raw)
	case "socks", "socks5":
		return parseSocks(raw, "socks")
	case "http", "https":
		return parseSocks(raw, "http") // Handles user:pass logic similarly
	case "wireguard":
		return parseWireGuard(raw)
	case "hysteria2", "hy2":
		return parseHysteria2(raw)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", scheme)
	}
}

// --- VMess ---
type vmessJSON struct {
	V    interface{} `json:"v"`
	Ps   string      `json:"ps"`
	Add  string      `json:"add"`
	Port interface{} `json:"port"`
	Id   string      `json:"id"`
	Aid  interface{} `json:"aid"`
	Scy  string      `json:"scy"`
	Net  string      `json:"net"`
	Type string      `json:"type"`
	Host string      `json:"host"`
	Path string      `json:"path"`
	Tls  string      `json:"tls"`
	Sni  string      `json:"sni"`
	Alpn string      `json:"alpn"`
	Fp   string      `json:"fp"`
}

func parseVMess(raw string) (*Profile, error) {
	// Standard VMess URI (vmess://...?...)
	if strings.Contains(raw, "?") && strings.Contains(raw, "&") {
		return parseVLESS(raw) // Logic is identical to VLESS for query params
	}

	// Base64 JSON (Legacy)
	b64 := strings.TrimPrefix(raw, "vmess://")
	jsonStr, err := DecodeBase64(b64)
	if err != nil {
		return nil, fmt.Errorf("vmess base64 error: %w", err)
	}

	var v vmessJSON
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return nil, fmt.Errorf("vmess json error: %w", err)
	}

	p := &Profile{
		Protocol:    "vmess",
		RawURI:      raw,
		Remarks:     v.Ps,
		Address:     v.Add,
		Password:    v.Id,
		Method:      v.Scy,
		Network:     v.Net,
		Host:        v.Host,
		Path:        v.Path,
		Security:    v.Tls,
		SNI:         v.Sni,
		Fingerprint: v.Fp,
	}

	if p.Method == "" {
		p.Method = "auto"
	}

	// Port handling (can be string or int in JSON)
	p.Port, _ = strconv.Atoi(fmt.Sprintf("%v", v.Port))

	// Map generic "Type" to specific fields
	p.HeaderType = v.Type
	if p.Network == "grpc" {
		p.Mode = v.Type // "gun" or "multi" often in 'type'
		p.ServiceName = v.Path
	}
	if p.Network == "kcp" {
		p.Seed = v.Path
	}

	return p, nil
}

// --- VLESS, Trojan, Hysteria2 (Generic URI) ---
func parseVLESS(raw string) (*Profile, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	p := &Profile{
		Protocol: u.Scheme,
		RawURI:   raw,
		Address:  u.Hostname(),
		Remarks:  u.Fragment,
		Password: u.User.String(),
	}
	port, _ := strconv.Atoi(u.Port())
	p.Port = port

	q := u.Query()
	ParseQueryParam(p, q)

	// Fix: Encryption logic for VLESS
	if p.Protocol == "vless" {
		p.Method = q.Get("encryption")
		if p.Method == "" {
			p.Method = "none"
		}
	} else if p.Protocol == "vmess" {
		// If we ended up here via VMess standard parsing
		p.Method = "auto"
	}

	return p, nil
}

func parseTrojan(raw string) (*Profile, error) {
	p, err := parseVLESS(raw)
	if err != nil {
		return nil, err
	}
	p.Protocol = "trojan"
	if p.Network == "" {
		p.Network = "tcp"
	}
	return p, nil
}

func parseHysteria2(raw string) (*Profile, error) {
	p, err := parseVLESS(raw)
	if err != nil {
		return nil, err
	}
	p.Protocol = "hysteria2"

	q := p.getParsedQuery(raw)
	p.ObfsPassword = q.Get("obfs-password")
	p.PortHopping = q.Get("mport")
	if p.ObfsPassword != "" {
		p.Obfs = "salamander"
	}
	return p, nil
}

// Helper to re-parse query since parseVLESS consumes it
func (p *Profile) getParsedQuery(raw string) url.Values {
	u, _ := url.Parse(raw)
	return u.Query()
}

// --- Shadowsocks ---
func parseShadowsocks(raw string) (*Profile, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	p := &Profile{
		Protocol: "shadowsocks",
		RawURI:   raw,
		Address:  u.Hostname(),
		Remarks:  u.Fragment,
	}
	p.Port, _ = strconv.Atoi(u.Port())

	userInfo := u.User.String()

	// SIP002 Logic: If no colon, Base64 decode the whole block
	if !strings.Contains(userInfo, ":") {
		decoded, err := DecodeBase64(userInfo)
		if err == nil {
			userInfo = decoded
		}
	}

	// Split method:password
	parts := strings.SplitN(userInfo, ":", 2)
	if len(parts) == 2 {
		p.Method = parts[0]
		p.Password = parts[1]
	} else {
		// Fallback or invalid
		return nil, fmt.Errorf("invalid shadowsocks userinfo")
	}

	// Plugin Logic (v2rayNG logic)
	q := u.Query()
	plugin := q.Get("plugin")
	if strings.Contains(plugin, "obfs=http") {
		// Need to parse complex plugin strings "obfs-local;obfs=http;obfs-host=..."
		p.Network = "tcp"
		p.HeaderType = "http"

		// Regex to find embedded params
		if match := regexp.MustCompile(`obfs-host=([^;]+)`).FindStringSubmatch(plugin); len(match) > 1 {
			p.Host = match[1]
		}
		if match := regexp.MustCompile(`path=([^;]+)`).FindStringSubmatch(plugin); len(match) > 1 {
			p.Path = match[1]
		}
	}

	return p, nil
}

// --- Socks / HTTP ---
func parseSocks(raw string, proto string) (*Profile, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	p := &Profile{
		Protocol: proto,
		RawURI:   raw,
		Address:  u.Hostname(),
		Remarks:  u.Fragment,
	}
	p.Port, _ = strconv.Atoi(u.Port())

	if u.User != nil {
		p.Username = u.User.Username()
		p.Password, _ = u.User.Password()
	}
	return p, nil
}

// --- WireGuard ---
func parseWireGuard(raw string) (*Profile, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	p := &Profile{
		Protocol:  "wireguard",
		RawURI:    raw,
		Address:   u.Hostname(),
		Remarks:   u.Fragment,
		SecretKey: u.User.String(), // Private Key in user info
	}
	p.Port, _ = strconv.Atoi(u.Port())

	q := u.Query()
	p.PublicKey = q.Get("publickey")
	p.PreSharedKey = q.Get("presharedkey")
	p.LocalAddress = q.Get("address")
	if p.LocalAddress == "" {
		p.LocalAddress = "172.16.0.2/32" // Default IPv4
	}

	if mtuStr := q.Get("mtu"); mtuStr != "" {
		p.MTU, _ = strconv.Atoi(mtuStr)
	}

	// Reserved bytes "1,2,3" -> [1,2,3]
	if reserved := q.Get("reserved"); reserved != "" {
		parts := strings.Split(reserved, ",")
		for _, part := range parts {
			val, _ := strconv.Atoi(strings.TrimSpace(part))
			p.Reserved = append(p.Reserved, uint8(val))
		}
	}

	return p, nil
}
