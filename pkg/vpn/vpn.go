package vpn

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	ErrNotConnected = errors.New("not connected to VPN")
	ErrInvalidRegion = errors.New("invalid region format")
	ErrConnectFail  = errors.New("failed to connect to VPN")
)

type Region struct {
	Provider string
	Country  string
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