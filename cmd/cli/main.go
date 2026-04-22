package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kqzz/MCsniperGO/claimer"
	"github.com/Kqzz/MCsniperGO/cmd/cli/droptime"
	"github.com/Kqzz/MCsniperGO/log"
	"github.com/Kqzz/MCsniperGO/pkg/mc"
	"github.com/Kqzz/MCsniperGO/pkg/parser"
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
	fmt.Print("\x1B7")     // Save the cursor position
	fmt.Print("\x1B[2K")   // Erase the entire line - breaks smth else so idk
	fmt.Print("\x1B[0J")   // Erase from cursor to end of screen
	fmt.Print("\x1B[?47h") // Save screen
	// fmt.Print("\x1B[1J")   // Erase from cursor to beginning of screen
	fmt.Print("\x1B[?47l") // Restore screen

	fmt.Printf("\x1B[%d;%dH", 0, 0) // move cursor to row #, col #

	elapsed := time.Since(startTime).Seconds()

	requestsPerSecond := float64(claimer.Stats.Total) / elapsed

	fmt.Printf("[RPS: %.2f | DUPLICATE: %d | NOT_ALLOWED: %d | TOO_MANY_REQUESTS: %d]     ", requestsPerSecond, claimer.Stats.Duplicate, claimer.Stats.NotAllowed, claimer.Stats.TooManyRequests)
	fmt.Print("\x1B8") // Restore the cursor position util new size is calculated
}

func main() {

	var startUsername string
	flag.StringVar(&startUsername, "username", "", "username to snipe")
	flag.StringVar(&startUsername, "u", "", "username to snipe")
	flag.BoolVar(&autoDroptimeMode, "auto-droptime", false, "auto snipe 3-char usernames from 3name.xyz")
	flag.BoolVar(&autoDroptimeMode, "3", false, "auto snipe 3-char usernames from 3name.xyz")
	flag.BoolVar(&disableBar, "disable-bar", false, "disables status bar")
	if isFlagPassed("disable-bar") {
		disableBar = true
	}

	flag.Parse()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		fmt.Print("\r")
		log.Log("err", "ctrl-c pressed, exiting...      ")
		os.Exit(0)
	}()

	log.Log("", log.GetHeader())

	var rotator *vpn.Rotator
	var proxies []string

	vpnRegions, err := vpn.LoadRegions("vpn.txt")
	if err == nil && len(vpnRegions) > 0 {
		vpnConfig, _ := vpn.LoadConfig("vpn_config.txt")
		rotator, err = vpn.NewRotator(vpnRegions, vpnConfig)
		if err == nil && rotator != nil {
			log.Log("info", "loaded %d VPN regions", len(vpnRegions))
		} else {
			log.Log("err", "failed to create VPN rotator: %v", err)
		}
	}

	if rotator == nil {
		proxies, err = parser.ReadLines("proxies.txt")
		if err != nil {
			log.Log("err", "failed to load proxies: %v", err)
		} else {
			log.Log("info", "loaded %d proxies (fallback mode)", len(proxies))
		}
	} else {
		auth, authErr := vpn.LoadAuth("vpn_auth.txt")
		if authErr != nil {
			log.Log("warn", "VPN auth not configured, using system CLI auth")
		} else {
			if auth.MullvadAccount != "" {
				log.Log("info", "authenticating Mullvad...")
				mullvad := vpn.NewMullvadProvider()
				if err := mullvad.Authenticate(auth.MullvadAccount); err != nil {
					log.Log("err", "Mullvad auth failed: %v", err)
				} else {
					log.Log("info", "Mullvad authenticated")
				}
			}
			if auth.ProtonEmail != "" && auth.ProtonPassword != "" {
				log.Log("info", "authenticating Proton VPN...")
				proton := vpn.NewProtonProvider()
				if err := proton.Authenticate(auth.ProtonEmail, auth.ProtonPassword); err != nil {
					log.Log("err", "Proton auth failed: %v", err)
				} else {
					log.Log("info", "Proton VPN authenticated")
				}
			}
		}

		err = rotator.Connect()
		if err != nil {
			log.Log("err", "failed to connect to VPN: %v", err)
			rotator = nil
			proxies, _ = parser.ReadLines("proxies.txt")
			log.Log("info", "falling back to proxies")
		} else {
			log.Log("info", "connected to VPN: %s", rotator.CurrentRegion())
		}
	}

	accounts, err := getAccounts("gc.txt", "gp.txt", "ms.txt")

	if err != nil {
		log.Log("err", "fatal: %v", err)
		log.Input("press enter to continue")
		return
	}

	if autoDroptimeMode {
		runAutoDroptime(accounts, proxies, rotator)
		return
	}

	var username string

	if !isFlagPassed("u", "username") {
		username = log.Input("target username")
	} else {
		username = startUsername
	}

	dropRange := log.GetDropRange()

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
		drops, err := droptime.FetchDroptimes()
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
