package vpn

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const WireguardConfigPath = "/tmp/wireguard"

type WireguardProvider struct {
	mu          sync.Mutex
	connected   bool
	currentIP   string
	currentConf string
}

func NewWireguardProvider() *WireguardProvider {
	return &WireguardProvider{}
}

func (p *WireguardProvider) Name() string {
	return "wireguard"
}

func (p *WireguardProvider) Connect(configName string) error {
	if !hasWgQuick() {
		return fmt.Errorf("wg-quick not found. Install wireguard-tools")
	}

	configName = strings.TrimSuffix(configName, ".conf")
	configPath := filepath.Join(WireguardConfigPath, configName+".conf")

	if _, err := os.Stat(configPath); err != nil {
		return fmt.Errorf("config not found: %s", configPath)
	}

	cmd := exec.Command("wg-quick", "up", configPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wg-quick up failed: %s, %v", string(out), err)
	}

	p.currentConf = configName

	time.Sleep(500 * time.Millisecond)

	for i := 0; i < 10; i++ {
		if p.IsConnected() {
			ip, _ := p.GetCurrentIP()
			p.currentIP = ip
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("connection timeout")
}

func (p *WireguardProvider) Disconnect() error {
	if p.currentConf == "" {
		return nil
	}

	configPath := filepath.Join(WireguardConfigPath, p.currentConf+".conf")

	cmd := exec.Command("wg-quick", "down", configPath)
	_, err := cmd.CombinedOutput()
	if err == nil {
		p.connected = false
		p.currentIP = ""
		p.currentConf = ""
	}
	return err
}

func (p *WireguardProvider) GetCurrentRegion() (Region, error) {
	if !p.connected {
		return Region{}, ErrNotConnected
	}
	return Region{
		Provider: "wireguard",
		Country:  p.currentConf,
	}, nil
}

func (p *WireguardProvider) GetCurrentIP() (string, error) {
	if !p.connected {
		return "", ErrNotConnected
	}

	if p.currentIP != "" {
		return p.currentIP, nil
	}

	cmd := exec.Command("wg", "show", "wireguard.conf", "endpoint")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, ":") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	cmd = exec.Command("hostname", "-I")
	out, err = cmd.Output()
	if err == nil {
		ips := strings.Split(string(out), " ")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0]), nil
		}
	}

	return "", fmt.Errorf("could not determine IP")
}

func (p *WireguardProvider) IsConnected() bool {
	if !hasWgQuick() {
		return false
	}

	cmd := exec.Command("wg", "show")
	_, err := cmd.Output()
	if err != nil {
		return false
	}

	output, _ := cmd.Output()
	p.connected = strings.Contains(string(output), "interface")
	return p.connected
}

func hasWgQuick() bool {
	_, err := exec.LookPath("wg-quick")
	if err == nil {
		return true
	}
	_, err = exec.LookPath("wg")
	return err == nil
}