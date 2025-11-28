package xray

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"rizznet/internal/logger"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"

	// Import distro to register all protocols/transports
	_ "github.com/xtls/xray-core/main/distro/all"
)

// StartEphemeral spins up Xray on a random available port for a single link.
func StartEphemeral(link string) (int, *core.Instance, error) {
	portsMap, instance, err := StartMultiEphemeral([]string{link})
	if err != nil {
		return 0, nil, err
	}
	return portsMap[link], instance, nil
}

// StartMultiEphemeral starts a single Xray instance hosting multiple proxies.
func StartMultiEphemeral(links []string) (map[string]int, *core.Instance, error) {
	// 1. Mute ALL Logs (Stdout/Stderr) for the duration of this function.
	// This hides Xray's internal deprecation warnings during validation & startup.
	restoreLogs := muteLogs()
	defer restoreLogs()

	count := len(links)
	if count == 0 {
		return nil, nil, fmt.Errorf("no links provided")
	}

	// 2. Get Ports
	ports, err := getFreePorts(count)
	if err != nil {
		return nil, nil, err
	}

	var inbounds []conf.InboundDetourConfig
	var outbounds []conf.OutboundDetourConfig
	var rules []json.RawMessage

	linkToPort := make(map[string]int)
	validIndex := 0

	for _, link := range links {
		// A. Parse
		outConfig, err := ToXrayConfig(link)
		if err != nil {
			logger.Log.Debugf("Skipping invalid link: %v", err)
			continue
		}

		// B. XRAY VALIDATION (Dry Run)
		// This is where the Deprecation Warnings were coming from.
		// Now that we muted logs at the start of the function, these will be hidden.
		if _, err := outConfig.Build(); err != nil {
			continue
		}

		if validIndex >= len(ports) {
			break
		}
		port := ports[validIndex]

		tagIn := fmt.Sprintf("in_%d", validIndex)
		tagOut := fmt.Sprintf("out_%d", validIndex)

		outConfig.Tag = tagOut
		outbounds = append(outbounds, *outConfig)

		inbounds = append(inbounds, conf.InboundDetourConfig{
			Tag:      tagIn,
			Protocol: "socks",
			PortList: &conf.PortList{Range: []conf.PortRange{{From: uint32(port), To: uint32(port)}}},
			Settings: toRawMessagePtr(`{"auth": "noauth", "udp": true}`),
			ListenOn: toAddress("127.0.0.1"),
		})

		ruleMap := map[string]interface{}{
			"type":        "field",
			"inboundTag":  []string{tagIn},
			"outboundTag": tagOut,
		}
		ruleJSON, _ := json.Marshal(ruleMap)
		rules = append(rules, json.RawMessage(ruleJSON))

		linkToPort[link] = port
		validIndex++
	}

	if len(outbounds) == 0 {
		return nil, nil, fmt.Errorf("no valid links in batch")
	}

	// 3. Build & Start
	pbConfig, err := (&conf.Config{
		LogConfig:       &conf.LogConfig{LogLevel: "none"},
		InboundConfigs:  inbounds,
		OutboundConfigs: outbounds,
		RouterConfig: &conf.RouterConfig{
			RuleList: rules,
		},
	}).Build()

	if err != nil {
		return nil, nil, err
	}

	instance, err := core.New(pbConfig)
	if err != nil {
		return nil, nil, err
	}

	if err := instance.Start(); err != nil {
		return nil, nil, err
	}

	return linkToPort, instance, nil
}

// muteLogs redirects stdout and stderr to /dev/null and returns a function to restore them.
func muteLogs() func() {
	origStdout := os.Stdout
	origStderr := os.Stderr

	// Open /dev/null
	devNull, _ := os.Open(os.DevNull)

	// If open fails, we just don't mute.
	if devNull != nil {
		os.Stdout = devNull
		os.Stderr = devNull
	}

	return func() {
		if devNull != nil {
			devNull.Close()
		}
		os.Stdout = origStdout
		os.Stderr = origStderr
	}
}

func getFreePorts(count int) ([]int, error) {
	var listeners []net.Listener
	var ports []int

	for i := 0; i < count; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			for _, l := range listeners {
				l.Close()
			}
			return nil, fmt.Errorf("failed to allocate ports: %w", err)
		}
		listeners = append(listeners, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}

	for _, l := range listeners {
		l.Close()
	}

	return ports, nil
}

func toAddress(s string) *conf.Address {
	var addr conf.Address
	_ = json.Unmarshal([]byte(fmt.Sprintf("%q", s)), &addr)
	return &addr
}
