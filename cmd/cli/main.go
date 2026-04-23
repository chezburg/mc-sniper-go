package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Kqzz/MCsniperGO/claimer"
	"github.com/Kqzz/MCsniperGO/cmd/cli/droptime"
	"github.com/Kqzz/MCsniperGO/log"
	"github.com/Kqzz/MCsniperGO/pkg/config"
	"github.com/Kqzz/MCsniperGO/pkg/mc"
	"github.com/Kqzz/MCsniperGO/pkg/vpn"
)

const help = `usage:
    mcsnipergo [options]
options:
    --username, -u <str>    username to snipe
    --auto-droptime, -3      auto snipe 3-char usernames from 3name.xyz
	--disable-bar           disables the status bar
`

var disableBar bool
var autoDroptimeMode bool
var dryTestMode bool

func init() {
	flag.Usage = func() {
		fmt.Print(help)
	}
}

func isFlagPassed(names ...string) bool {
	found := false
	for _, name := range names {
		flag.Visit(func(f *flag.Flag) {
			if f.Name == name {
				found = true
			}
		})
	}
	return found
}

func statusBar(startTime time.Time) {
	fmt.Print("\x1B7")
	fmt.Print("\x1B[2K")
	fmt.Print("\x1B[0J")
	fmt.Print("\x1B[?47h")
	fmt.Print("\x1B[?47l")

	fmt.Printf("\x1B[%d;%dH", 0, 0)

	elapsed := time.Since(startTime).Seconds()
	requestsPerSecond := float64(claimer.Stats.Total) / elapsed

	fmt.Printf("[RPS: %.2f | DUPLICATE: %d | NOT_ALLOWED: %d | TOO_MANY_REQUESTS: %d]     ", requestsPerSecond, claimer.Stats.Duplicate, claimer.Stats.NotAllowed, claimer.Stats.TooManyRequests)
	fmt.Print("\x1B8")
}

func main() {

	var startUsername string
	flag.StringVar(&startUsername, "username", "", "username to snipe")
	flag.StringVar(&startUsername, "u", "", "username to snipe")
	flag.BoolVar(&autoDroptimeMode, "auto-droptime", false, "auto snipe 3-char usernames from 3name.xyz")
	flag.BoolVar(&autoDroptimeMode, "3", false, "auto snipe 3-char usernames from 3name.xyz")
	flag.BoolVar(&disableBar, "disable-bar", false, "disables status bar")
	flag.BoolVar(&dryTestMode, "dry-test", false, "test accounts and VPN without sniping")
	flag.BoolVar(&dryTestMode, "d", false, "test accounts and VPN without sniping")
	if isFlagPassed("disable-bar") {
		disableBar = true
	}

	flag.Parse()

	if startUsername == "" && !isFlagPassed("auto-droptime", "3") {
		autoDroptimeMode = true
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		fmt.Print("\r")
		log.Log("err", "ctrl-c pressed, exiting...      ")
		os.Exit(0)
	}()

	log.Log("", log.GetHeader())

cfg := config.Load()
	var rotator *vpn.Rotator
	var proxies []string

	if cfg.WIREGUARD_PRIVATE_KEY != "" && cfg.VPNServiceProvider == config.ProviderMullvad {
		log.Log("info", "using Mullvad WireGuard")

		regions := make([]vpn.VPNRegion, 0)
		if len(cfg.SERVER_COUNTRIES) > 0 {
			for _, country := range strings.Split(cfg.SERVER_COUNTRIES, ",") {
				country = strings.TrimSpace(country)
				if country != "" {
					regions = append(regions, vpn.VPNRegion{Provider: "wireguard", Country: country})
				}
			}
		}
		if len(regions) == 0 {
			regions = append(regions, vpn.VPNRegion{Provider: "wireguard", Country: "ca"})
		}

		wgProvider := vpn.NewMullvadWireguardProvider(
			cfg.WIREGUARD_PRIVATE_KEY,
			cfg.WIREGUARD_ADDRESSES,
			cfg.MULLVAD_ACCOUNT,
		)
		rotator, _ = vpn.NewRotatorWithProvider(regions, &vpn.RotatorConfig{}, wgProvider)
	} else {
		vpnRegions := cfg.GetVPNRegions()
		if len(vpnRegions) > 0 {
			rotatorCfg := &vpn.RotatorConfig{
				MaxRequestsPerRegion:  cfg.VPN_MAX_REQUESTS_PER_REGION,
				MinRotationInterval:   cfg.VPN_MIN_ROTATION_INTERVAL,
				DetectOn429:         cfg.VPN_DETECT_ON_429,
				Predictive:           cfg.VPN_PREDICTIVE,
				FallbackToProxies:    cfg.VPN_FALLBACK_TO_PROXIES,
				MaxRateLimitHits:     cfg.VPN_MAX_RATELIMIT_HITS,
				PredictiveThreshold:  cfg.VPN_PREDICTIVE_THRESHOLD,
			}

			regions := make([]vpn.VPNRegion, len(vpnRegions))
			for i, r := range vpnRegions {
				regions[i] = vpn.VPNRegion{
					Provider: r.Provider,
					Country:  r.Country,
				}
			}

			var err error
			rotator, err = vpn.NewRotator(regions, rotatorCfg)
			if err == nil && rotator != nil {
				log.Log("info", "loaded %d VPN regions", len(vpnRegions))
			} else {
				log.Log("err", "failed to create VPN rotator: %v", err)
			}
		}
	}

	if rotator == nil {
		proxies = cfg.GetProxies()
		if len(proxies) > 0 {
			log.Log("info", "loaded %d proxies (fallback mode)", len(proxies))
		} else {
			log.Log("info", "no VPN or proxies configured")
		}
	}

	if rotator != nil {
		err := rotator.Connect()
		if err != nil {
			log.Log("err", "failed to connect to VPN: %v", err)
			rotator = nil
			proxies = cfg.GetProxies()
			log.Log("info", "falling back to proxies")
		} else {
			log.Log("info", "connected to VPN: %s", rotator.CurrentRegion())
		}
	} else {
		proxies = cfg.GetProxies()
		if len(proxies) > 0 {
			log.Log("info", "no VPN configured, using proxies")
		} else {
			log.Log("info", "no VPN or proxies configured")
		}
	}

	accounts, err := getAccounts("gc.txt", "gp.txt", "ms.txt")

	if err != nil {
		log.Log("err", "fatal: %v", err)
		log.Input("press enter to continue")
		return
	}

	if dryTestMode {
		ok := testVPNAndAccounts(accounts, rotator)
		if !ok {
			os.Exit(1)
		}
		return
	}

	var username string

	if !isFlagPassed("u", "username") && !autoDroptimeMode {
		username = log.Input("target username")
	} else {
		username = startUsername
	}

	if autoDroptimeMode {
		runAutoDroptime(accounts, proxies, rotator)
		return
	}

	var dropRange mc.DropRange
	if username != "" {
		log.Log("info", "fetching droptime for %s...", username)
		dropInfo, err := droptime.FetchDropInfo(username, proxies)
		if err == nil {
			dropRange = mc.DropRange{
				Start: dropInfo.DropStart,
				End:   dropInfo.DropEnd,
			}
			log.Log("success", "found droptime: %s", dropRange.Start.Format("15:04:05"))
		} else {
			log.Log("warn", "could not auto-fetch droptime: %v", err)
			dropRange = log.GetDropRange()
		}
	} else {
		dropRange = log.GetDropRange()
	}

	go func() {

		if disableBar {
			return
		}

		if dropRange.Start.After(time.Now()) {
			time.Sleep(time.Until(dropRange.Start))
		}

		start := dropRange.Start
		if start.Before(time.Now()) {
			start = time.Now()
		}

		for {
			statusBar(start)
			time.Sleep(time.Second * 1)
		}
	}()

	err = claimer.ClaimWithinRange(username, dropRange, accounts, proxies, rotator)

	if err != nil {
		log.Log("err", "fatal: %v", err)
	}

	log.Input("snipe completed, press enter to continue")

}

func runAutoDroptime(accounts []*mc.MCaccount, proxies []string, rotator *vpn.Rotator) {
	for {
		drops, err := droptime.FetchDroptimes(proxies)
		if err != nil {
			log.Log("err", "failed to fetch droptimes: %v", err)
			log.Input("press enter to continue")
			return
		}

		if len(drops) == 0 {
			log.Log("warn", "no names found, refetching in 5 minutes...")
			time.Sleep(5 * time.Minute)
			continue
		}

		log.Log("info", "found %d names to snipe", len(drops))

		for _, drop := range drops {
			now := time.Now()
			if drop.DropStart.After(now) {
				waitDuration := time.Until(drop.DropStart)
				log.Log("info", "waiting %v for %s to drop", waitDuration, drop.Username)
				time.Sleep(waitDuration)
			}

			if !disableBar {
				go func() {
					for {
						statusBar(drop.DropStart)
						time.Sleep(time.Second * 1)
					}
				}()
			}

			dropRange := mc.DropRange{
				Start: drop.DropStart,
				End:   drop.DropEnd,
			}

			log.Log("info", "sniping %s (drop window: %v)", drop.Username, drop.DropEnd.Sub(drop.DropStart))
			err = claimer.ClaimWithinRange(drop.Username, dropRange, accounts, proxies, rotator)
			if err != nil {
				log.Log("err", "claim error: %v", err)
			}

			time.Sleep(2 * time.Second)
		}

		log.Log("info", "all names processed, refetching list...")
	}
}