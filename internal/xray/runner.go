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

// StartMultiEphemeral starts a single Xray instance hosting multiple proxies on random free ports.
func StartMultiEphemeral(links []string) (map[string]int, *core.Instance, error) {
	count := len(links)
	if count == 0 {
		return nil, nil, fmt.Errorf("no links provided")
	}

	ports, err := GetFreePorts(count)
	if err != nil {
		return nil, nil, err
	}

	return StartOnPorts(links, ports)
}

// StartOnPorts starts Xray using a pre-defined set of ports.
// The length of links must not exceed the length of ports.
func StartOnPorts(links []string, ports []int) (portMap map[string]int, instance *core.Instance, err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Log.Errorf("CRITICAL: Xray Core Panic recovered: %v", r)
			err = fmt.Errorf("xray core panic: %v", r)
			if instance != nil {
				instance.Close()
				instance = nil
			}
		}
	}()

	// CHANGED: Removed muteLogs() to ensure runtime visibility for debugging

	if len(links) > len(ports) {
		return nil, nil, fmt.Errorf("not enough ports provided: have %d, need %d", len(ports), len(links))
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

		var buildErr error
		func() {
			restore := muteLogs()
			defer restore()
			_, buildErr = outConfig.Build()
		}()

		if buildErr != nil {
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
		LogConfig: &conf.LogConfig{
			LogLevel:      "none",
			AccessLog:     "none",
			ErrorLog:      "none",
			DNSLog:        false,
		},
		InboundConfigs:  inbounds,
		OutboundConfigs: outbounds,
		RouterConfig: &conf.RouterConfig{
			RuleList: rules,
		},
	}).Build()

	if err != nil {
		return nil, nil, err
	}

	instance, err = core.New(pbConfig)
	if err != nil {
		return nil, nil, err
	}

	if err := instance.Start(); err != nil {
		return nil, nil, err
	}

	return linkToPort, instance, nil
}

func GetFreePorts(count int) ([]int, error) {
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

func muteLogs() func() {
	origStdout := os.Stdout
	origStderr := os.Stderr

	devNull, _ := os.Open(os.DevNull)
	if devNull != nil {
		os.Stdout = devNull
		os.Stderr = devNull
	}

	return func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
		if devNull != nil {
			devNull.Close()
		}
	}
}
