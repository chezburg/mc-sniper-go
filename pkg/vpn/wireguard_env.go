package vpn

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const MullvadAPIURL = "https://api.mullvad.net/www/relays/all/"

type MullvadRelay struct {
	Hostname     string `json:"hostname"`
	CountryCode  string `json:"country_code"`
	CountryName string `json:"country_name"`
	CityCode    string `json:"city_code"`
	CityName   string `json:"city_name"`
	IPv4AddrIn  string `json:"ipv4_addr_in"`
	IPv6AddrIn string `json:"ipv6_addr_in"`
	Pubkey      string `json:"pubkey"`
	Type        string `json:"type"`
	Active      bool   `json:"active"`
}

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
	mullvad    bool
	mullvadAccount string
}

func NewWireguardEnvProvider(privateKey, address, endpoint, publicKey string) *WireguardEnvProvider {
	return &WireguardEnvProvider{
		privateKey: privateKey,
		address:   address,
		endpoint:  endpoint,
		publicKey: publicKey,
	}
}

func NewMullvadWireguardProvider(privateKey, address, account string) *WireguardEnvProvider {
	return &WireguardEnvProvider{
		privateKey:      privateKey,
		address:       address,
		mullvad:       true,
		mullvadAccount: account,
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

	if p.mullvad {
		return p.connectMullvad(country)
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
		interfaceConfig := fmt.Sprintf("PrivateKey = %s\n", p.privateKey)
		if p.address != "" {
			interfaceConfig += fmt.Sprintf("Address = %s\n", p.address)
		}

		peerConfig := ""
		if p.publicKey != "" {
			peerConfig += fmt.Sprintf("PublicKey = %s\n", p.publicKey)
		}
		if p.endpoint != "" {
			peerConfig += fmt.Sprintf("Endpoint = %s\n", p.endpoint)
		}
		peerConfig += `AllowedIPs = 0.0.0.0/1, 128.0.0.0/1
PersistentKeepalive = 25
`

		wgConfig = fmt.Sprintf(`[Interface]
%s[Peer]
%s`, interfaceConfig, peerConfig)
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

	if p.endpoint != "" {
		if err := p.addEndpointRoute(p.endpoint); err != nil {
			fmt.Printf("[!] Warning: failed to add endpoint route: %v\n", err)
		}
	}

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

func (p *WireguardEnvProvider) connectMullvad(country string) error {
	relays, err := fetchMullvadRelays()
	if err != nil {
		return fmt.Errorf("failed to fetch mullvad relays: %v", err)
	}

	countryLower := strings.ToLower(country)
	var relay *MullvadRelay
	for _, r := range relays {
		if r.Type == "wireguard" && r.Active {
			if strings.ToLower(r.CountryCode) == countryLower {
				relay = &r
				break
			}
		}
	}

	if relay == nil {
		for _, r := range relays {
			if r.Type == "wireguard" && r.Active && strings.Contains(strings.ToLower(r.CountryName), countryLower) {
				relay = &r
				break
			}
		}
	}
	if relay == nil {
		return fmt.Errorf("no wireguard relay found for country: %s", country)
	}

	if p.address == "" {
		return fmt.Errorf("wireguard address not set. Download wireguard config from Mullvad and set WIREGUARD_ADDRESSES (e.g., 10.64.x.x/32)")
	}

	configName := "wg0"
	configPath := filepath.Join(WireguardConfigPath, configName+".conf")

	if err := os.MkdirAll(WireguardConfigPath, 0755); err != nil {
		return fmt.Errorf("failed to create wireguard dir: %v", err)
	}

	wgConfig := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s

[Peer]
PublicKey = %s
Endpoint = %s:51820
AllowedIPs = 0.0.0.0/1, 128.0.0.0/1
PersistentKeepalive = 25
`, p.privateKey, p.address, relay.Pubkey, relay.IPv4AddrIn)

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
	p.endpoint = relay.IPv4AddrIn
	p.publicKey = relay.Pubkey

	// Add specific route for the endpoint to avoid routing loop with /1 hack
	if err := p.addEndpointRoute(relay.IPv4AddrIn); err != nil {
		fmt.Printf("[!] Warning: failed to add endpoint route: %v. VPN might not route traffic.\n", err)
	} else {
		fmt.Printf("[*] Added specific route for VPN endpoint %s to avoid loop\n", relay.IPv4AddrIn)
	}

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

func fetchMullvadRelays() ([]MullvadRelay, error) {
	req, err := http.NewRequest("GET", MullvadAPIURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var relays []MullvadRelay
	if err := json.Unmarshal(body, &relays); err != nil {
		return nil, err
	}

	return relays, nil
}

func (p *WireguardEnvProvider) fetchMullvadAddress() (string, error) {
	pubKey, err := generatePublicKey(p.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to derive public key: %v", err)
	}

	form := url.Values{}
	form.Add("account", p.mullvadAccount)
	form.Add("pubkey", pubKey)

	req, err := http.NewRequest("POST", "https://api.mullvad.net/wg/", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("mullvad API returned status %d: %s", resp.StatusCode, string(body))
	}

	return strings.TrimSpace(string(body)), nil
}

func generatePublicKey(privateKey string) (string, error) {
	return "", fmt.Errorf("public key derivation not implemented - provide MULLVAD_ACCOUNT for address lookup")
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

func (p *WireguardEnvProvider) addEndpointRoute(endpoint string) error {
	// Strip port if present
	host := endpoint
	if h, _, err := net.SplitHostPort(endpoint); err == nil {
		host = h
	}

	// Resolve hostname if necessary
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("failed to resolve endpoint %s: %w", host, err)
	}
	
	if len(ips) == 0 {
		return fmt.Errorf("no IPs found for endpoint %s", host)
	}
	
	targetIP := ips[0].String()

	// Get current default gateway
	cmd := exec.Command("ip", "route", "show", "default")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get default route: %w", err)
	}

	// Output format example: "default via 192.168.128.1 dev eth0"
	parts := strings.Fields(string(out))
	var gateway string
	var iface string
	for i, part := range parts {
		if part == "via" && i+1 < len(parts) {
			gateway = parts[i+1]
		}
		if part == "dev" && i+1 < len(parts) {
			iface = parts[i+1]
		}
	}

	if gateway != "" && iface != "" {
		fmt.Printf("[*] Detected gateway %s on %s, adding route for endpoint %s (%s)\n", gateway, iface, host, targetIP)
		// ip route add <endpoint> via <gateway> dev <iface>
		cmd = exec.Command("ip", "route", "add", targetIP, "via", gateway, "dev", iface)
		return cmd.Run()
	}
	
	return fmt.Errorf("could not detect default gateway from: %s", string(out))
}

func GenerateWireguardConfig(privateKey, address, publicKey, endpoint string) string {
	interfaceConfig := fmt.Sprintf("PrivateKey = %s\n", privateKey)
	if address != "" {
		interfaceConfig += fmt.Sprintf("Address = %s\n", address)
	}

	peerConfig := ""
	if publicKey != "" {
		peerConfig += fmt.Sprintf("PublicKey = %s\n", publicKey)
	}
	if endpoint != "" {
		peerConfig += fmt.Sprintf("Endpoint = %s\n", endpoint)
	}
	peerConfig += `AllowedIPs = 0.0.0.0/1, 128.0.0.0/1
PersistentKeepalive = 25
`

	return fmt.Sprintf(`[Interface]
%s[Peer]
%s`, interfaceConfig, peerConfig)
}