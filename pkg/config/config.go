package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type VPNServiceProvider string

const (
	ProviderMullvad  VPNServiceProvider = "mullvad"
	ProviderProtonVPN VPNServiceProvider = "protonvpn"
	ProviderWireGuard VPNServiceProvider = "wireguard"
)

type VPNType string

const (
	VPNTypeWireGuard VPNType = "wireguard"
	VPNTypeOpenVPN   VPNType = "openvpn"
)

type Config struct {
	VPNServiceProvider VPNServiceProvider
	VPNType            VPNType

	WIREGUARD_PRIVATE_KEY string
	WIREGUARD_ADDRESSES    string
	MULLVAD_ACCOUNT   string

	SERVER_COUNTRIES  string
	SERVER_REGIONS    string
	SERVER_CITIES     string
	SERVER_HOSTNAMES  string

	OPENVPN_USER     string
	OPENVPN_PASSWORD string

	PROXIES string

	GC_ACCOUNTS string
	GP_ACCOUNTS string
	MS_ACCOUNTS string

	VPN_MAX_REQUESTS_PER_REGION  int
	VPN_MIN_ROTATION_INTERVAL   time.Duration
	VPN_DETECT_ON_429           bool
	VPN_PREDICTIVE             bool
	VPN_FALLBACK_TO_PROXIES    bool
	VPN_MAX_RATELIMIT_HITS     int
	VPN_PREDICTIVE_THRESHOLD   int
}

func Load() *Config {
	cfg := &Config{}

	cfg.VPNServiceProvider = VPNServiceProvider(strings.ToLower(os.Getenv("VPN_SERVICE_PROVIDER")))
	cfg.VPNType = VPNType(strings.ToLower(os.Getenv("VPN_TYPE")))

	cfg.WIREGUARD_PRIVATE_KEY = os.Getenv("WIREGUARD_PRIVATE_KEY")
	cfg.WIREGUARD_ADDRESSES = os.Getenv("WIREGUARD_ADDRESSES")
	cfg.MULLVAD_ACCOUNT = os.Getenv("MULLVAD_ACCOUNT")

	cfg.SERVER_COUNTRIES = os.Getenv("SERVER_COUNTRIES")
	cfg.SERVER_REGIONS = os.Getenv("SERVER_REGIONS")
	cfg.SERVER_CITIES = os.Getenv("SERVER_CITIES")
	cfg.SERVER_HOSTNAMES = os.Getenv("SERVER_HOSTNAMES")

	cfg.OPENVPN_USER = os.Getenv("OPENVPN_USER")
	cfg.OPENVPN_PASSWORD = os.Getenv("OPENVPN_PASSWORD")

	cfg.PROXIES = os.Getenv("PROXIES")

	cfg.GC_ACCOUNTS = os.Getenv("GC_ACCOUNTS")
	cfg.GP_ACCOUNTS = os.Getenv("GP_ACCOUNTS")
	cfg.MS_ACCOUNTS = os.Getenv("MS_ACCOUNTS")

	cfg.VPN_MAX_REQUESTS_PER_REGION = parseEnvInt("VPN_MAX_REQUESTS_PER_REGION", 25)
	cfg.VPN_MIN_ROTATION_INTERVAL = parseEnvDuration("VPN_MIN_ROTATION_INTERVAL", 5*time.Second)
	cfg.VPN_DETECT_ON_429 = parseEnvBool("VPN_DETECT_ON_429", true)
	cfg.VPN_PREDICTIVE = parseEnvBool("VPN_PREDICTIVE", true)
	cfg.VPN_FALLBACK_TO_PROXIES = parseEnvBool("VPN_FALLBACK_TO_PROXIES", true)
	cfg.VPN_MAX_RATELIMIT_HITS = parseEnvInt("VPN_MAX_RATELIMIT_HITS", 2)
	cfg.VPN_PREDICTIVE_THRESHOLD = parseEnvInt("VPN_PREDICTIVE_THRESHOLD", 80)

	return cfg
}

func (c *Config) GetVPNRegions() []VPNRegion {
	var regions []VPNRegion

	if c.SERVER_HOSTNAMES != "" {
		for _, h := range splitLines(c.SERVER_HOSTNAMES) {
			regions = append(regions, VPNRegion{
				Provider: string(c.VPNServiceProvider),
				Country:  h,
			})
		}
		return regions
	}

	if c.SERVER_CITIES != "" {
		for _, city := range splitLines(c.SERVER_CITIES) {
			regions = append(regions, VPNRegion{
				Provider: string(c.VPNServiceProvider),
				Country:  strings.ToLower(city),
			})
		}
		return regions
	}

	if c.SERVER_COUNTRIES != "" {
		for _, country := range splitLines(c.SERVER_COUNTRIES) {
			regions = append(regions, VPNRegion{
				Provider: string(c.VPNServiceProvider),
				Country:  strings.ToLower(country),
			})
		}
		return regions
	}

	return regions
}

func (c *Config) GetProxies() []string {
	if c.PROXIES == "" {
		return nil
	}
	return splitLines(c.PROXIES)
}

func (c *Config) GetGCAccounts() []string {
	if c.GC_ACCOUNTS == "" {
		return nil
	}
	return splitLines(c.GC_ACCOUNTS)
}

func (c *Config) GetGPAccounts() []string {
	if c.GP_ACCOUNTS == "" {
		return nil
	}
	return splitLines(c.GP_ACCOUNTS)
}

func (c *Config) GetMSAccounts() []string {
	if c.MS_ACCOUNTS == "" {
		return nil
	}
	return splitLines(c.MS_ACCOUNTS)
}

func (c *Config) HasVPNAuth() bool {
	return c.WIREGUARD_PRIVATE_KEY != "" ||
		(c.OPENVPN_USER != "" && c.OPENVPN_PASSWORD != "")
}

func parseEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return parsed
}

func parseEnvDuration(key string, defaultVal time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return defaultVal
	}
	return d
}

func parseEnvBool(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	return val == "true" || val == "1" || val == "yes"
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, ",") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

type VPNRegion struct {
	Provider string
	Country  string
}