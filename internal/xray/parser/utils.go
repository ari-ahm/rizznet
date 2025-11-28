package parser

import (
	"encoding/base64"
	"net/url"
	"strings"
)

// DecodeBase64 attempts to decode standard and URL-safe base64 strings,
// automatically fixing missing padding.
func DecodeBase64(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	// Fix padding
	if n := len(s) % 4; n != 0 {
		s += strings.Repeat("=", 4-n)
	}

	// Try Standard
	b, err := base64.StdEncoding.DecodeString(s)
	if err == nil {
		return string(b), nil
	}

	// Try URL-Safe
	b, err = base64.URLEncoding.DecodeString(s)
	if err == nil {
		return string(b), nil
	}

	return "", err
}

// FixIllegalUrl cleans up common issues in scraped links.
func FixIllegalUrl(s string) string {
	s = strings.TrimSpace(s)
	// Handle missing protocol slash or spaces
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// ParseQueryParam extracts standard transport/security params from query values.
// This mimics the `getItemFormQuery` logic in v2rayNG.
func ParseQueryParam(p *Profile, q url.Values) {
	if v := q.Get("type"); v != "" {
		p.Network = v
	}
	if v := q.Get("headerType"); v != "" {
		p.HeaderType = v
	}
	if v := q.Get("host"); v != "" {
		p.Host = v
	}
	if v := q.Get("path"); v != "" {
		p.Path = v
	}
	if v := q.Get("seed"); v != "" {
		p.Seed = v
	}
	if v := q.Get("quicSecurity"); v != "" {
		p.QuicSecurity = v
	}
	if v := q.Get("key"); v != "" {
		p.QuicKey = v
	}
	if v := q.Get("mode"); v != "" {
		p.Mode = v
	}
	if v := q.Get("serviceName"); v != "" {
		p.ServiceName = v
	}
	if v := q.Get("authority"); v != "" {
		p.Authority = v
	}
	if v := q.Get("security"); v != "" {
		p.Security = v
	}
	if v := q.Get("sni"); v != "" {
		p.SNI = v
	}
	if v := q.Get("fp"); v != "" {
		p.Fingerprint = v
	}
	if v := q.Get("alpn"); v != "" {
		p.ALPN = strings.Split(v, ",")
	}
	if v := q.Get("pbk"); v != "" {
		p.Pbk = v
	}
	if v := q.Get("sid"); v != "" {
		p.Sid = v
	}
	if v := q.Get("spx"); v != "" {
		p.SpiderX = v
	}
	if v := q.Get("flow"); v != "" {
		p.Flow = v
	}

	// Insecure mapping (1/0/true/false)
	allowInsecure := []string{"allowInsecure", "insecure", "allow_insecure"}
	for _, key := range allowInsecure {
		if val := q.Get(key); val != "" {
			p.Insecure = (val == "1" || val == "true")
			break
		}
	}
}
