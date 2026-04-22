package vpn

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type RotatorConfig struct {
	MaxRequestsPerRegion  int
	MinRotationInterval time.Duration
	DetectOn429         bool
	Predictive          bool
	FallbackToProxies   bool
	MaxRateLimitHits   int
	PredictiveThreshold int
}

type VPNRegion struct {
	Provider string
	Country  string
}

type Rotator struct {
	mu                     sync.Mutex
	providers               map[string]*VPNManager
	regions                []VPNRegion
	currentIdx              int
	requestsSinceRotate    atomic.Int64
	rateLimitHits          atomic.Int64
	predictedLimit         int
	lastRotateTime         time.Time
	config                *RotatorConfig
	connected             bool
}

func LoadRegions(path string) ([]VPNRegion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var regions []VPNRegion
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		region, err := ParseRegion(line)
		if err != nil {
			continue
		}

		regions = append(regions, VPNRegion{
			Provider: region.Provider,
			Country:  region.Country,
		})
	}

	return regions, nil
}

func NewRotator(regions []VPNRegion, config *RotatorConfig) (*Rotator, error) {
	if config == nil {
		config = defaultRotatorConfig()
	}

	r := &Rotator{
		regions:        regions,
		predictedLimit: config.MaxRequestsPerRegion,
		config:        config,
		providers:    make(map[string]*VPNManager),
	}

	for _, region := range regions {
		providerKey := region.Provider
		if _, ok := r.providers[providerKey]; !ok {
			mgr, err := NewVPNManager(region.Provider)
			if err != nil {
				continue
			}
			r.providers[providerKey] = mgr
		}
	}

	return r, nil
}

func (r *Rotator) Connect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.currentIdx >= len(r.regions) {
		r.currentIdx = 0
	}

	region := r.regions[r.currentIdx]
	mgr, ok := r.providers[region.Provider]
	if !ok {
		return fmt.Errorf("no provider for %s", region.Provider)
	}

	vpnRegion := Region{
		Provider: region.Provider,
		Country:  region.Country,
	}

	err := Connect(mgr, vpnRegion)
	if err != nil {
		return err
	}

	r.connected = true
	r.lastRotateTime = time.Now()
	r.requestsSinceRotate.Store(0)
	r.rateLimitHits.Store(0)

	return nil
}

func (r *Rotator) Disconnect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, mgr := range r.providers {
		Disconnect(mgr)
	}

	r.connected = false
	return nil
}

func (r *Rotator) CurrentRegion() VPNRegion {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentIdx >= len(r.regions) || r.currentIdx < 0 {
		return VPNRegion{}
	}
	return r.regions[r.currentIdx]
}

func (r *Rotator) RecordRequest(statusCode int, responseTime time.Duration) {
	r.requestsSinceRotate.Add(1)

	if statusCode == 429 && r.config.DetectOn429 {
		r.rateLimitHits.Add(1)

		requests := int(r.requestsSinceRotate.Load())
		if requests > 0 {
			newLimit := requests * 80 / 100
			if newLimit < r.predictedLimit {
				r.predictedLimit = newLimit
			}
		}
	}
}

func (r *Rotator) ShouldRotate() bool {
	if !r.config.DetectOn429 {
		return false
	}

	if r.rateLimitHits.Load() >= int64(r.config.MaxRateLimitHits) {
		return true
	}

	requests := int(r.requestsSinceRotate.Load())
	threshold := r.predictedLimit * r.config.PredictiveThreshold / 100

	if requests >= threshold && requests > 0 {
		return true
	}

	if time.Since(r.lastRotateTime) < r.config.MinRotationInterval {
		return false
	}

	if requests >= r.predictedLimit {
		return true
	}

	return false
}

func (r *Rotator) Rotate() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.currentIdx++
	if r.currentIdx >= len(r.regions) {
		r.currentIdx = 0
	}

	region := r.regions[r.currentIdx]
	mgr, ok := r.providers[region.Provider]
	if !ok {
		return fmt.Errorf("no provider for %s", region.Provider)
	}

	vpnRegion := Region{
		Provider: region.Provider,
		Country:  region.Country,
	}

	err := Connect(mgr, vpnRegion)
	if err != nil {
		return err
	}

	r.lastRotateTime = time.Now()
	r.requestsSinceRotate.Store(0)
	r.rateLimitHits.Store(0)

	return nil
}

func (r *Rotator) IsConnected() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connected
}

func (r *Rotator) RequestsSinceRotate() int64 {
	return r.requestsSinceRotate.Load()
}

func (r *Rotator) RateLimitHits() int64 {
	return r.rateLimitHits.Load()
}

func LoadConfig(path string) (*RotatorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultRotatorConfig(), nil
	}

	return parseRotatorConfig(string(data)), nil
}

func parseRotatorConfig(data string) *RotatorConfig {
	cfg := defaultRotatorConfig()

	for _, line := range strings.Split(data, "\n") {
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
		case "max_requests_per_region":
			fmt.Sscanf(value, "%d", &cfg.MaxRequestsPerRegion)
		case "min_rotation_interval":
			if d, err := time.ParseDuration(value); err == nil {
				cfg.MinRotationInterval = d
			}
		case "detect_on_429":
			cfg.DetectOn429 = value == "true"
		case "predictive":
			cfg.Predictive = value == "true"
		case "fallback_to_proxies":
			cfg.FallbackToProxies = value == "true"
		case "max_ratelimit_hits":
			fmt.Sscanf(value, "%d", &cfg.MaxRateLimitHits)
		case "predictive_threshold":
			fmt.Sscanf(value, "%d", &cfg.PredictiveThreshold)
		}
	}

	return cfg
}

func defaultRotatorConfig() *RotatorConfig {
	return &RotatorConfig{
		MaxRequestsPerRegion:  25,
		MinRotationInterval:   5 * time.Second,
		DetectOn429:           true,
		Predictive:            true,
		FallbackToProxies:     true,
		MaxRateLimitHits:     2,
		PredictiveThreshold:  80,
	}
}