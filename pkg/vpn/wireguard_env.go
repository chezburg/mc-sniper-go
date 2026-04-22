package vpn

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type WireguardEnvProvider struct {
	mu          sync.Mutex
	connected   bool
	currentIP  string
	currentConf string
	privateKey  string
	address    string
	endpoint   string
	publicKey  string
	presetConf string
}

func NewWireguardEnvProvider(privateKey, address, endpoint, publicKey string) *WireguardEnvProvider {
	return &WireguardEnvProvider{
		privateKey: privateKey,
		address:   address,
		endpoint:  endpoint,
		publicKey: publicKey,
	}
}

func NewWireguardEnvProviderFromConfig(presetConf string) *WireguardEnvProvider {
	return &WireguardEnvProvider{
		presetConf: presetConf,
	}
}

func (p *WireguardEnvProvider) Name() string {
	return "wireguard"
}

func (p *WireguardEnvProvider) Connect(country string) error {
	if !hasWgQuick() {
		return fmt.Errorf("wg-quick not found. Install wireguard-tools")
	}

	configName := "wg0"
	configPath := filepath.Join(WireguardConfigPath, configName+".conf")

	if err := os.MkdirAll(WireguardConfigPath, 0755); err != nil {
		return fmt.Errorf("failed to create wireguard dir: %v", err)
	}

	var wgConfig string
	if p.presetConf != "" {
		wgConfig = p.presetConf
	} else {
		wgConfig = fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s

[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`, p.privateKey, p.address, p.publicKey, p.endpoint)
	}

	if err := os.WriteFile(configPath, []byte(wgConfig), 0600); err != nil {
		return fmt.Errorf("failed to write wireguard config: %v", err)
	}

	cmd := exec.Command("wg-quick", "up", configPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(configPath)
		return fmt.Errorf("wg-quick up failed: %s, %v", string(out), err)
	}

	p.currentConf = configName
	p.currentIP = extractIP(p.address)

	time.Sleep(500 * time.Millisecond)

	for i := 0; i < 10; i++ {
		if p.IsConnected() {
			ip, _ := p.GetCurrentIP()
			if ip != "" {
				p.currentIP = ip
			}
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	p.Disconnect()
	return fmt.Errorf("connection timeout")
}

func (p *WireguardEnvProvider) Disconnect() error {
	if p.currentConf == "" {
		return nil
	}

	configPath := filepath.Join(WireguardConfigPath, p.currentConf+".conf")

	cmd := exec.Command("wg-quick", "down", configPath)
	_, err := cmd.CombinedOutput()
	if err == nil {
		p.connected = false
		p.currentIP = ""
	}
	os.Remove(configPath)
	p.currentConf = ""
	return err
}

func (p *WireguardEnvProvider) GetCurrentRegion() (Region, error) {
	if !p.connected {
		return Region{}, ErrNotConnected
	}
	return Region{
		Provider: "wireguard",
		Country:  p.endpoint,
	}, nil
}

func (p *WireguardEnvProvider) GetCurrentIP() (string, error) {
	if !p.connected {
		return "", ErrNotConnected
	}

	if p.currentIP != "" {
		return p.currentIP, nil
	}

	cmd := exec.Command("wg")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "endpoint:") {
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

func (p *WireguardEnvProvider) IsConnected() bool {
	if !hasWgQuick() {
		return false
	}

	cmd := exec.Command("ip", "link", "show", "wg0")
	_, err := cmd.Output()
	if err == nil {
		p.connected = true
		return true
	}

	for i := 0; i < 10; i++ {
		iface := fmt.Sprintf("wg-%d", time.Now().UnixNano()-int64(i*1000000))
		cmd = exec.Command("ip", "link", "show", iface)
		_, err = cmd.Output()
		if err == nil {
			p.connected = true
			return true
		}
	}

	cmd = exec.Command("wg")
	_, err = cmd.Output()
	return err == nil
}

func extractIP(address string) string {
	if ip := net.ParseIP(strings.Split(address, "/")[0]); ip != nil {
		return ip.String()
	}
	return ""
}

func GenerateWireguardConfig(privateKey, address, publicKey, endpoint string) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s

[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`, privateKey, address, publicKey, endpoint)
}