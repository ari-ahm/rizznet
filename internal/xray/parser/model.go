package parser

// Profile represents a normalized proxy configuration derived from any protocol link.
// It acts as a Data Transfer Object (DTO) between raw URIs and Xray config.
type Profile struct {
	Protocol string // vmess, vless, trojan, shadowsocks, socks, http, wireguard, hysteria2
	RawURI   string
	Remarks  string

	// Connection Details
	Address string
	Port    int

	// Authentication
	Username string // User for Socks/HTTP
	Password string // UUID, Key, Password
	Method   string // Encryption method (SS/VMess)

	// WireGuard Specifics
	SecretKey    string // Private Key
	LocalAddress string
	PublicKey    string
	PreSharedKey string
	MTU          int
	Reserved     []uint8 // Parsed from "0,0,0"

	// Hysteria2 Specifics
	Obfs         string // "salamander"
	ObfsPassword string
	PortHopping  string // mport

	// Transport (StreamSettings)
	Network      string // tcp, kcp, ws, http, grpc, quic
	HeaderType   string // none, http, wireguard, etc
	Host         string // Request Host
	Path         string // WS/HTTP Path
	Seed         string // KCP Seed
	QuicSecurity string
	QuicKey      string
	Mode         string // GRPC mode (gun)
	ServiceName  string // GRPC ServiceName
	Authority    string // GRPC Authority

	// Security (TLS/REALITY)
	Security    string // tls, reality, none
	Insecure    bool   // AllowInsecure
	SNI         string
	Fingerprint string   // fp
	ALPN        []string // alpn

	// REALITY Specifics
	Pbk     string // PublicKey
	Sid     string // ShortId
	SpiderX string // spx
	Flow    string // xtls-rprx-vision
}
