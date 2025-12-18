package xray

import (
	"bufio"
	"regexp"
	"strings"

	"rizznet/internal/xray/parser"
)

var regexLink = regexp.MustCompile(`(vmess|vless|trojan|ss|socks|socks5|http|https|wireguard|hysteria2|hy2)://[a-zA-Z0-9_\-\.\:@\?=&%#+/]+`)

func ExtractLinks(text string) []string {
	if !strings.Contains(text, "://") {
		cleanBase64 := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, text)

		if decoded, err := parser.DecodeBase64(cleanBase64); err == nil {
			if strings.Contains(decoded, "://") {
				text = decoded
			}
		}
	}

	var links []string
	text = strings.ReplaceAll(text, "\r\n", "\n")
	scanner := bufio.NewScanner(strings.NewReader(text))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		matches := regexLink.FindAllString(line, -1)
		for _, match := range matches {
			clean := strings.TrimRight(match, ".,;)\"")
			if clean != "" {
				links = append(links, clean)
			}
		}
	}
	return deduplicate(links)
}

func deduplicate(input []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range input {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}
