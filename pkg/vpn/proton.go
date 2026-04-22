package vpn

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ProtonProvider struct {
	connected   bool
	currentIP   string
	currentNode string
}

func NewProtonProvider() *ProtonProvider {
	return &ProtonProvider{}
}

func (p *ProtonProvider) Name() string {
	return "proton"
}

func (p *ProtonProvider) Authenticate(email, password string) error {
	if !hasProtonCLI() {
		return fmt.Errorf("protonvpn CLI not found. Install from protonvpn.com")
	}

	if email == "" || password == "" {
		cmd := exec.Command("protonvpn", "status")
		_, err := cmd.Output()
		if err == nil {
			return nil
		}
		return fmt.Errorf("protonvpn not logged in. Run: protonvpn login")
	}

	cmd := exec.Command("protonvpn", "login", "-n", email)
	cmd.Stdin = strings.NewReader(password + "\n")
	_, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("protonvpn auth failed: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("protonvpn auth failed: %v", err)
	}

	return nil
}

func (p *ProtonProvider) Connect(country string) error {
	if !hasProtonCLI() {
		return fmt.Errorf("protonvpn CLI not found. Install from protonvpn.com")
	}

	cmd := exec.Command("protonvpn", "connect", country, "-f", "1")
	_, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("protonvpn connect failed: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("protonvpn connect failed: %v", err)
	}

	p.currentNode = country

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

func (p *ProtonProvider) Disconnect() error {
	if !hasProtonCLI() {
		return nil
	}

	cmd := exec.Command("protonvpn", "disconnect")
	_, err := cmd.Output()
	if err == nil {
		p.connected = false
		p.currentIP = ""
		p.currentNode = ""
	}
	return err
}

func (p *ProtonProvider) GetCurrentRegion() (Region, error) {
	if !p.connected {
		return Region{}, ErrNotConnected
	}
	return Region{
		Provider: "proton",
		Country: p.currentNode,
	}, nil
}

func (p *ProtonProvider) GetCurrentIP() (string, error) {
	if !p.connected {
		return "", ErrNotConnected
	}

	if p.currentIP != "" {
		return p.currentIP, nil
	}

	cmd := exec.Command("protonvpn", "status")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "IP:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	return "", fmt.Errorf("could not determine IP")
}

func (p *ProtonProvider) IsConnected() bool {
	if !hasProtonCLI() {
		return false
	}

	cmd := exec.Command("protonvpn", "status")
	out, err := cmd.Output()
	if err != nil {
		return false
	}

	status := strings.ToLower(string(out))
	p.connected = strings.Contains(status, "connected")
	return p.connected
}

func hasProtonCLI() bool {
	if _, err := exec.LookPath("protonvpn"); err == nil {
		return true
	}
	if _, err := exec.LookPath("protonvpnd"); err == nil {
		return true
	}
	return false
}