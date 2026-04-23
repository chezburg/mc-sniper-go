package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Kqzz/MCsniperGO/log"
	"github.com/Kqzz/MCsniperGO/pkg/config"
	"github.com/Kqzz/MCsniperGO/pkg/mc"
	"github.com/Kqzz/MCsniperGO/pkg/parser"
	"github.com/Kqzz/MCsniperGO/pkg/vpn"
)

func getAccounts(giftCodePath string, gamepassPath string, microsoftPath string) ([]*mc.MCaccount, error) {
	accounts, err := getAccountsFromEnv()
	if err != nil {
		return nil, err
	}
	
	if len(accounts) > 0 {
		return accounts, nil
	}

	giftCodeLines, _ := parser.ReadLines(giftCodePath)
	gamepassLines, _ := parser.ReadLines(gamepassPath)
	microsoftLines, _ := parser.ReadLines(microsoftPath)

	return parseAccountsFromLines(giftCodeLines, gamepassLines, microsoftLines)
}

func getAccountsFromEnv() ([]*mc.MCaccount, error) {
	var accounts []*mc.MCaccount
	tokensPath := "tokens.json"
	if os.Getenv("TOKENS_PATH") != "" {
		tokensPath = os.Getenv("TOKENS_PATH")
	}

	tokensMap := mc.LoadTokensMap(tokensPath)

	bearerToken := os.Getenv("MC_BEARER_TOKEN")
	email := os.Getenv("MC_EMAIL")
	password := os.Getenv("MC_PASSWORD")

	if bearerToken != "" {
		acc := &mc.MCaccount{
			Email:      email,
			Type:       mc.Ms,
			Bearer:     bearerToken,
			TokensPath: tokensPath,
		}
		acc.DefaultFastHttpHandler()
		accounts = append(accounts, acc)
		log.Log("info", "Loaded account from MC_BEARER_TOKEN env var")
	}

	if email != "" && password != "" {
		acc := &mc.MCaccount{
			Email:      email,
			Password:   password,
			Type:       mc.Ms,
			TokensPath: tokensPath,
		}
		
		// Check for cached token
		if td, ok := tokensMap[email]; ok && td.Bearer != "" {
			acc.Bearer = td.Bearer
			acc.RefreshToken = td.RefreshToken
			log.Log("info", "Loaded cached token for %s", email)
		}

		acc.DefaultFastHttpHandler()
		accounts = append(accounts, acc)
		log.Log("info", "Loaded account from MC_EMAIL/MC_PASSWORD env vars")
	}

	cfg := config.Load()

	gcAccounts := cfg.GetGCAccounts()
	for _, cred := range gcAccounts {
		parts := strings.SplitN(cred, ":", 2)
		if len(parts) != 2 {
			continue
		}
		acc := &mc.MCaccount{
			Email:      parts[0],
			Password:   parts[1],
			Type:       mc.MsPr,
			TokensPath: tokensPath,
		}
		
		if td, ok := tokensMap[acc.Email]; ok && td.Bearer != "" {
			acc.Bearer = td.Bearer
			acc.RefreshToken = td.RefreshToken
		}

		acc.DefaultFastHttpHandler()
		accounts = append(accounts, acc)
		log.Log("info", "Loaded GC account %s", acc.Email)
	}

	gpAccounts := cfg.GetGPAccounts()
	for _, cred := range gpAccounts {
		parts := strings.SplitN(cred, ":", 2)
		if len(parts) != 2 {
			continue
		}
		acc := &mc.MCaccount{
			Email:      parts[0],
			Password:   parts[1],
			Type:       mc.MsGp,
			TokensPath: tokensPath,
		}

		if td, ok := tokensMap[acc.Email]; ok && td.Bearer != "" {
			acc.Bearer = td.Bearer
			acc.RefreshToken = td.RefreshToken
		}

		acc.DefaultFastHttpHandler()
		accounts = append(accounts, acc)
		log.Log("info", "Loaded GP account %s", acc.Email)
	}

	msAccounts := cfg.GetMSAccounts()
	for _, cred := range msAccounts {
		if cred == "" {
			continue
		}
		
		var acc *mc.MCaccount
		if strings.Contains(cred, ":") {
			parts := strings.SplitN(cred, ":", 2)
			acc = &mc.MCaccount{
				Email:      parts[0],
				Password:   parts[1],
				Type:       mc.Ms,
				TokensPath: tokensPath,
			}
			if td, ok := tokensMap[acc.Email]; ok && td.Bearer != "" {
				acc.Bearer = td.Bearer
				acc.RefreshToken = td.RefreshToken
			}
		} else {
			acc = &mc.MCaccount{
				Type:       mc.Ms,
				Bearer:     cred,
				TokensPath: tokensPath,
			}
		}

		acc.DefaultFastHttpHandler()
		accounts = append(accounts, acc)
		log.Log("info", "Loaded MS account %s", acc.Email)
	}

	// Initial auth/refresh for all accounts that need it
	for _, acc := range accounts {
		if acc.Bearer == "" && acc.Password != "" {
			if acc.RefreshToken != "" {
				err := acc.RefreshAuthenticate()
				if err != nil {
					log.Log("warn", "Failed to refresh token for %s: %v. Falling back to headless...", acc.Email, err)
					acc.HeadlessAuthenticate(true)
				}
			} else {
				acc.HeadlessAuthenticate(true)
			}
		} else if acc.RefreshToken != "" {
			// Optional: force refresh on start to ensure it's fresh
			log.Log("info", "Refreshing token for %s on startup...", acc.Email)
			acc.RefreshAuthenticate()
		}
	}

	return accounts, nil
}

func getAccountsFromLines(gcLines, gpLines, msLines []string) ([]*mc.MCaccount, error) {
	return parseAccountsFromLines(gcLines, gpLines, msLines)
}

func parseAccountsFromLines(gcLines, gpLines, msLines []string) ([]*mc.MCaccount, error) {
	gcs, parseErrors := parser.ParseAccounts(gcLines, mc.MsPr)

	for _, er := range parseErrors {
		if er == nil {
			continue
		}
		log.Log("err", "%v", er)
	}
	microsofts, msParseErrors := parser.ParseAccounts(msLines, mc.Ms)

	for _, er := range msParseErrors {
		if er == nil {
			continue
		}
		log.Log("err", "%v", er)
	}

	gamepasses, gpParseErrors := parser.ParseAccounts(gpLines, mc.MsGp)

	for _, er := range gpParseErrors {
		if er == nil {
			continue
		}

	}

	accounts := append(gcs, microsofts...)
	accounts = append(accounts, gamepasses...)

	if len(accounts) == 0 {
		return accounts, fmt.Errorf("no accounts found")
	}

	return accounts, nil
}

func testVPNAndAccounts(accounts []*mc.MCaccount, rotator *vpn.Rotator) bool {
	vpnOk := testVPNConnections(rotator)
	accountsOk := testAccounts(accounts)

	fmt.Println()
	if !vpnOk {
		fmt.Println("[DRY-TEST] FAILED: VPN is not functioning")
	}
	if !accountsOk {
		fmt.Println("[DRY-TEST] FAILED: No accounts are working")
	}

	if !vpnOk || !accountsOk {
		return false
	}

	fmt.Println("[DRY-TEST] PASSED: VPN and accounts are functional")
	return true
}

func testAccounts(accounts []*mc.MCaccount) bool {
	if len(accounts) == 0 {
		fmt.Println("[DRY-TEST] Accounts: No accounts to test")
		return false
	}

	workingAccounts := 0

	for _, account := range accounts {
		if account.Type != mc.Ms {
			continue
		}

		fmt.Printf("[DRY-TEST] Testing %s...", account.Email)

		if account.Bearer != "" {
			err := account.LoadAccountInfo()
			if err != nil {
				fmt.Printf(" FAIL: %v\n", err)
			} else {
				fmt.Printf(" PASS (user: %s)\n", account.Username)
				workingAccounts++
			}
			continue
		}

		// Otherwise try to authenticate using our new logic
		var err error
		if account.RefreshToken != "" {
			err = account.RefreshAuthenticate()
		} else if account.Password != "" {
			err = account.HeadlessAuthenticate(true)
		} else {
			err = fmt.Errorf("no credentials available")
		}

		if err != nil {
			fmt.Printf(" FAIL: %v\n", err)
		} else {
			err = account.LoadAccountInfo()
			if err != nil {
				fmt.Printf(" FAIL (info): %v\n", err)
			} else {
				fmt.Printf(" PASS (user: %s)\n", account.Username)
				workingAccounts++
			}
		}
	}

	if workingAccounts == 0 {
		fmt.Println("[DRY-TEST] Accounts: NONE functioning")
		return false
	}

	fmt.Printf("[DRY-TEST] Accounts: %d/%d working\n", workingAccounts, len(accounts))
	return workingAccounts > 0
}

func testVPNConnections(rotator *vpn.Rotator) bool {
	vpnConfigured := true

	cfg := config.Load()

	if rotator == nil {
		if cfg.WIREGUARD_PRIVATE_KEY != "" && cfg.VPNServiceProvider == config.ProviderMullvad {
			fmt.Println("[DRY-TEST] VPN: using Mullvad WireGuard")

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
			if len(vpnRegions) == 0 {
				fmt.Println("[DRY-TEST] VPN: no regions configured")
				vpnConfigured = false
			} else {
				rotatorCfg := &vpn.RotatorConfig{
					MaxRequestsPerRegion:   cfg.VPN_MAX_REQUESTS_PER_REGION,
					MinRotationInterval:  cfg.VPN_MIN_ROTATION_INTERVAL,
					DetectOn429:         cfg.VPN_DETECT_ON_429,
					Predictive:          cfg.VPN_PREDICTIVE,
					FallbackToProxies:   cfg.VPN_FALLBACK_TO_PROXIES,
					MaxRateLimitHits:    cfg.VPN_MAX_RATELIMIT_HITS,
					PredictiveThreshold: cfg.VPN_PREDICTIVE_THRESHOLD,
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
				if err != nil {
					fmt.Printf("[DRY-TEST] VPN: failed to create rotator: %v\n", err)
					return false
				}
			}
		}
	}

	if cfg.WIREGUARD_PRIVATE_KEY != "" {
		fmt.Println("[DRY-TEST] VPN: WireGuard key configured")
	}

	if rotator == nil {
		if vpnConfigured {
			fmt.Println("[DRY-TEST] VPN: rotator creation failed")
		}
		return false
	}

	err := rotator.Connect()
	if err != nil {
		fmt.Printf("[DRY-TEST] VPN connect: FAIL: %v\n", err)
		rotator.Disconnect()
		return false
	}

	region := rotator.CurrentRegion()
	if region.Provider != "" {
		fmt.Printf("[DRY-TEST] VPN connect: PASS (%s:%s)\n", region.Provider, region.Country)
	} else {
		fmt.Println("[DRY-TEST] VPN connect: PASS")
	}

	rotator.Disconnect()
	fmt.Println("[DRY-TEST] VPN: PASS")
	return true
}