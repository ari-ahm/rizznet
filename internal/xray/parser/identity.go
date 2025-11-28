package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// CalculateHash generates a unique identifier for the proxy configuration.
func (p *Profile) CalculateHash() string {
	var parts []string

	// --- 1. Basic Protocol & Endpoint ---
	parts = append(parts, strings.ToLower(p.Protocol))
	parts = append(parts, strings.ToLower(p.Address))
	parts = append(parts, fmt.Sprintf("%d", p.Port))

	// --- 2. Authentication ---
	parts = append(parts, p.Username)
	parts = append(parts, p.Password)
	parts = append(parts, p.SecretKey)

	// Normalization: Encryption Method
	// "none", "auto", or empty often mean the same thing depending on context.
	// But let's be specific per protocol to avoid collisions.
	method := strings.ToLower(p.Method)
	if p.Protocol == "vless" && method == "none" {
		method = "" // Normalize VLESS explicit 'none' to empty
	}
	parts = append(parts, method)

	// --- 3. Transport Specifics (Normalization is CRITICAL here) ---
	
	// Network: Empty implies "tcp"
	net := strings.ToLower(p.Network)
	if net == "" {
		net = "tcp"
	}
	parts = append(parts, net)

	// HeaderType: "none" implies empty/default
	header := strings.ToLower(p.HeaderType)
	if header == "none" {
		header = ""
	}
	parts = append(parts, header)

	parts = append(parts, p.Path)
	parts = append(parts, p.Mode)
	parts = append(parts, p.ServiceName)
	parts = append(parts, p.Seed)
	
	// --- 4. Advanced Protocol Specifics ---
	parts = append(parts, p.Flow)
	parts = append(parts, p.Obfs)
	parts = append(parts, p.ObfsPassword)
	
	// Reality keys are case-sensitive usually, but let's keep them as is.
	parts = append(parts, p.Pbk)
	parts = append(parts, p.Sid)
	
	// --- 5. WireGuard Specifics ---
	parts = append(parts, p.LocalAddress)
	parts = append(parts, p.PublicKey)
	
	// Sort and hash
	signature := strings.Join(parts, "|")
	hash := sha256.Sum256([]byte(signature))
	return hex.EncodeToString(hash[:])
}
