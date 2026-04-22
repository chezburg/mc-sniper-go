package vpn

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type MullvadProvider struct {
	connected    bool
	currentIP    string
	currentNode  string
}

func NewMullvadProvider() *MullvadProvider {
	return &MullvadProvider{}
}

func (p *MullvadProvider) Name() string {
	return "mullvad"
}

func (p *MullvadProvider) Authenticate(accountNumber string) error {
	if !hasMullvadCLI() {
		return fmt.Errorf("mullvad-vpn CLI not found. Install from mullvad.net")
	}

	if accountNumber == "" {
		cmd := exec.Command("mullvad", "account", "get")
		_, err := cmd.Output()
		if err == nil {
			return nil
		}
		return fmt.Errorf("mullvad account not set. Set with: mullvad account set ACCOUNT_NUMBER")
	}

	cmd := exec.Command("mullvad", "account", "set", accountNumber)
	_, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("mullvad auth failed: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("mullvad auth failed: %v", err)
	}

	return nil
}

func (p *MullvadProvider) Connect(country string) error {
	if !hasMullvadCLI() {
		return fmt.Errorf("mullvad-vpn CLI not found. Install from mullvad.net")
	}

	cmd := exec.Command("mullvad", "connect", country)
	_, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("mullvad connect failed: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("mullvad connect failed: %v", err)
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

func (p *MullvadProvider) Disconnect() error {
	if !hasMullvadCLI() {
		return nil
	}

	cmd := exec.Command("mullvad", "disconnect")
	_, err := cmd.Output()
	if err == nil {
		p.connected = false
		p.currentIP = ""
		p.currentNode = ""
	}
	return err
}

func (p *MullvadProvider) GetCurrentRegion() (Region, error) {
	if !p.connected {
		return Region{}, ErrNotConnected
	}
	return Region{
		Provider: "mullvad",
		Country:  p.currentNode,
	}, nil
}

func (p *MullvadProvider) GetCurrentIP() (string, error) {
	if !p.connected {
		return "", ErrNotConnected
	}

	if p.currentIP != "" {
		return p.currentIP, nil
	}

	cmd := exec.Command("mullvad", "status", "-v")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "IPv4:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	return "", fmt.Errorf("could not determine IP")
}

func (p *MullvadProvider) IsConnected() bool {
	if !hasMullvadCLI() {
		return false
	}

	cmd := exec.Command("mullvad", "status")
	out, err := cmd.Output()
	if err != nil {
		return false
	}

	status := strings.ToLower(string(out))
	p.connected = strings.Contains(status, "connected") || strings.Contains(status, "disconnected") == false
	return p.connected
}

func hasMullvadCLI() bool {
	_, err := exec.LookPath("mullvad")
	return err == nil
}