package xray

import (
	"encoding/json"
	"fmt"

	"rizznet/internal/xray/parser"

	"github.com/xtls/xray-core/infra/conf"
)

// ToXrayConfig converts a raw link string into an Xray outbound config.
func ToXrayConfig(raw string) (*conf.OutboundDetourConfig, error) {
	// 1. Parse into normalized Profile
	p, err := parser.Parse(raw)
	if err != nil {
		return nil, err
	}

	// 2. Build StreamSettings (Transport)
	streamSettings := buildStreamSettings(p)

	// 3. Build Protocol Specific Settings
	var protocol string
	var settings json.RawMessage

	switch p.Protocol {
	case "vmess":
		protocol = "vmess"
		settings = buildVMess(p)
	case "vless":
		protocol = "vless"
		settings = buildVLESS(p)
	case "trojan":
		protocol = "trojan"
		settings = buildTrojan(p)
	case "shadowsocks":
		protocol = "shadowsocks"
		settings = buildShadowsocks(p)
	case "socks", "socks5":
		protocol = "socks"
		settings = buildSocks(p)
	case "http", "https":
		protocol = "http"
		settings = buildHTTP(p)
	case "wireguard":
		protocol = "wireguard"
		settings = buildWireGuard(p)
	case "hysteria2", "hy2":
		// Native support in recent Xray versions
		protocol = "hysteria2"
		settings = buildHysteria2(p)
	default:
		return nil, fmt.Errorf("protocol conversion not implemented: %s", p.Protocol)
	}

	return &conf.OutboundDetourConfig{
		Tag:           "proxy",
		Protocol:      protocol,
		Settings:      &settings,
		StreamSetting: streamSettings,
	}, nil
}

// --- JSON Builders ---

func buildVMess(p *parser.Profile) json.RawMessage {
	return jsonRaw(map[string]interface{}{
		"vnext": []interface{}{
			map[string]interface{}{
				"address": p.Address,
				"port":    p.Port,
				"users": []interface{}{
					map[string]interface{}{
						"id":       p.Password,
						"alterId":  0,
						"security": p.Method,
					},
				},
			},
		},
	})
}

func buildVLESS(p *parser.Profile) json.RawMessage {
	return jsonRaw(map[string]interface{}{
		"vnext": []interface{}{
			map[string]interface{}{
				"address": p.Address,
				"port":    p.Port,
				"users": []interface{}{
					map[string]interface{}{
						"id":         p.Password,
						"encryption": p.Method,
						"flow":       p.Flow,
					},
				},
			},
		},
	})
}

func buildTrojan(p *parser.Profile) json.RawMessage {
	return jsonRaw(map[string]interface{}{
		"servers": []interface{}{
			map[string]interface{}{
				"address":  p.Address,
				"port":     p.Port,
				"password": p.Password,
			},
		},
	})
}

func buildShadowsocks(p *parser.Profile) json.RawMessage {
	return jsonRaw(map[string]interface{}{
		"servers": []interface{}{
			map[string]interface{}{
				"address":  p.Address,
				"port":     p.Port,
				"method":   p.Method,
				"password": p.Password,
			},
		},
	})
}

func buildSocks(p *parser.Profile) json.RawMessage {
	user := map[string]interface{}{}
	if p.Username != "" {
		user["user"] = p.Username
		user["pass"] = p.Password
	}

	// SOCKS structure requires "servers" array with optional users
	server := map[string]interface{}{
		"address": p.Address,
		"port":    p.Port,
	}
	if p.Username != "" {
		server["users"] = []interface{}{user}
	}

	return jsonRaw(map[string]interface{}{
		"servers": []interface{}{server},
	})
}

func buildHTTP(p *parser.Profile) json.RawMessage {
	server := map[string]interface{}{
		"address": p.Address,
		"port":    p.Port,
	}
	if p.Username != "" {
		server["users"] = []interface{}{
			map[string]interface{}{"user": p.Username, "pass": p.Password},
		}
	}
	return jsonRaw(map[string]interface{}{
		"servers": []interface{}{server},
	})
}

func buildWireGuard(p *parser.Profile) json.RawMessage {
	return jsonRaw(map[string]interface{}{
		"secretKey": p.SecretKey,
		"address":   []string{p.LocalAddress},
		"peers": []interface{}{
			map[string]interface{}{
				"publicKey":    p.PublicKey,
				"preSharedKey": p.PreSharedKey,
				"endpoint":     fmt.Sprintf("%s:%d", p.Address, p.Port),
			},
		},
		"reserved": p.Reserved,
		"mtu":      p.MTU,
	})
}

func buildHysteria2(p *parser.Profile) json.RawMessage {
	return jsonRaw(map[string]interface{}{
		"address": p.Address,
		"port":    p.Port,
		"auth":    p.Password,
		"obfs": map[string]interface{}{
			"type": p.Obfs, // "salamander"
			"salamander": map[string]interface{}{
				"password": p.ObfsPassword,
			},
		},
	})
}

func buildStreamSettings(p *parser.Profile) *conf.StreamConfig {
	// If WireGuard, stream settings are often empty/unused in Xray logic
	if p.Protocol == "wireguard" {
		return nil
	}

	if p.Network == "" {
		p.Network = "tcp"
	}

	sc := &conf.StreamConfig{
		Network:  (*conf.TransportProtocol)(&p.Network),
		Security: p.Security,
	}

	// TLS / REALITY
	if p.Security == "tls" || p.Security == "reality" {
		sc.TLSSettings = &conf.TLSConfig{
			ServerName:  p.SNI,
			Fingerprint: p.Fingerprint,
		}
		if len(p.ALPN) > 0 {
			sc.TLSSettings.ALPN = &conf.StringList{}
			*sc.TLSSettings.ALPN = append(*sc.TLSSettings.ALPN, p.ALPN...)
		}

		if p.Security == "reality" {
			sc.REALITYSettings = &conf.REALITYConfig{
				Fingerprint: p.Fingerprint,
				ServerName:  p.SNI,
				PublicKey:   p.Pbk,
				ShortId:     p.Sid,
				SpiderX:     p.SpiderX,
			}
		}

		if p.Insecure {
			sc.TLSSettings.Insecure = true
		}
	}

	// Transports
	switch p.Network {
	case "ws":
		sc.WSSettings = &conf.WebSocketConfig{
			Path: p.Path,
			Headers: map[string]string{
				"Host": p.Host,
			},
		}
	case "grpc":
		sc.GRPCSettings = &conf.GRPCConfig{
			ServiceName: p.ServiceName,
		}
		if p.Mode == "multi" {
			sc.GRPCSettings.MultiMode = true
		}
	case "tcp":
		if p.HeaderType == "http" {
			sc.TCPSettings = &conf.TCPConfig{
				HeaderConfig: jsonRaw(map[string]interface{}{
					"type": "http",
					"request": map[string]interface{}{
						"headers": map[string]interface{}{
							"Host": []string{p.Host},
						},
						"path": []string{p.Path},
					},
				}),
			}
		}
	case "kcp":
		sc.KCPSettings = &conf.KCPConfig{
			Seed: stringPtr(p.Seed),
		}
	}

	return sc
}

// --- Internal Helper Functions ---

func jsonRaw(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return json.RawMessage(b)
}

func stringPtr(s string) *string {
	return &s
}

// toRawMessagePtr is used by runner.go (and this package)
func toRawMessagePtr(s string) *json.RawMessage {
	msg := json.RawMessage(s)
	return &msg
}
