package vpn

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

var (
	ErrNotConnected = errors.New("not connected to VPN")
	ErrInvalidRegion = errors.New("invalid region format")
	ErrConnectFail  = errors.New("failed to connect to VPN")
	ErrAuthFailed  = errors.New("authentication failed")
)

type Region struct {
	Provider string
	Country  string
}

type VPNAuth struct {
	MullvadAccount string
	ProtonEmail  string
	ProtonPassword string
}

type Provider interface {
	Name() string
	Connect(region string) error
	Disconnect() error
	GetCurrentRegion() (Region, error)
	GetCurrentIP() (string, error)
	IsConnected() bool
}

type VPNManager struct {
	provider  Provider
	mu        sync.Mutex
	connected bool
	current  Region
}

func ParseRegion(line string) (Region, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return Region{}, ErrInvalidRegion
	}

	parts := strings.Split(line, ":")
	if len(parts) != 2 {
		return Region{}, ErrInvalidRegion
	}

	return Region{
		Provider: parts[0],
		Country:  strings.ToLower(parts[1]),
	}, nil
}

func NewVPNManager(providerName string) (*VPNManager, error) {
	var p Provider
	switch strings.ToLower(providerName) {
	case "mullvad":
		p = NewMullvadProvider()
	case "proton":
		p = NewProtonProvider()
	case "wireguard":
		p = NewWireguardProvider()
	default:
		return nil, fmt.Errorf("unsupported VPN provider: %s", providerName)
	}

	return &VPNManager{
		provider: p,
	}, nil
}

func Connect(mgr *VPNManager, region Region) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if mgr.connected {
		mgr.provider.Disconnect()
	}

	err := mgr.provider.Connect(region.Country)
	if err != nil {
		return err
	}

	mgr.connected = true
	mgr.current = region
	return nil
}

func Disconnect(mgr *VPNManager) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if !mgr.connected {
		return nil
	}

	err := mgr.provider.Disconnect()
	mgr.connected = false
	mgr.current = Region{}
	return err
}

func GetCurrentRegion(mgr *VPNManager) (Region, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if !mgr.connected {
		return Region{}, ErrNotConnected
	}

	return mgr.current, nil
}

func GetCurrentIP(mgr *VPNManager) (string, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if !mgr.connected {
		return "", ErrNotConnected
	}

	return mgr.provider.GetCurrentIP()
}

func IsConnected(mgr *VPNManager) bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return mgr.connected
}

func LoadAuth(path string) (*VPNAuth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("auth file not found: %v", err)
	}

	auth := &VPNAuth{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "mullvad_account":
			auth.MullvadAccount = value
		case "proton_email":
			auth.ProtonEmail = value
		case "proton_password":
			auth.ProtonPassword = value
		}
	}

	if auth.MullvadAccount == "" && auth.ProtonEmail == "" && auth.ProtonPassword == "" {
		return nil, fmt.Errorf("no VPN credentials found")
	}

	return auth, nil
}