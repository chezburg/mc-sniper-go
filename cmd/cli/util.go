package main

import (
	"fmt"

	"github.com/Kqzz/MCsniperGO/log"
	"github.com/Kqzz/MCsniperGO/pkg/mc"
	"github.com/Kqzz/MCsniperGO/pkg/parser"
	"github.com/Kqzz/MCsniperGO/pkg/vpn"
)

func getAccounts(giftCodePath string, gamepassPath string, microsoftPath string) ([]*mc.MCaccount, error) {
	giftCodeLines, _ := parser.ReadLines(giftCodePath)
	gamepassLines, _ := parser.ReadLines(gamepassPath)
	microsoftLines, _ := parser.ReadLines(microsoftPath)

	gcs, parseErrors := parser.ParseAccounts(giftCodeLines, mc.MsPr)

	for _, er := range parseErrors {
		if er == nil {
			continue
		}
		log.Log("err", "%v", er)
	}
	microsofts, msParseErrors := parser.ParseAccounts(microsoftLines, mc.Ms)

	for _, er := range msParseErrors {
		if er == nil {
			continue
		}
		log.Log("err", "%v", er)
	}

	gamepasses, gpParseErrors := parser.ParseAccounts(gamepassLines, mc.MsGp)

	for _, er := range gpParseErrors {
		if er == nil {
			continue
		}

	}

	accounts := append(gcs, microsofts...)
	accounts = append(accounts, gamepasses...)

	if len(accounts) == 0 {
		return accounts, fmt.Errorf("no accounts found in: gc.txt, ms.txt, gp.txt")
	}

	return accounts, nil
}

func testVPNAndAccounts(accounts []*mc.MCaccount, rotator *vpn.Rotator) {
	testVPNConnections(rotator)
	testAccounts(accounts)
}

func testAccounts(accounts []*mc.MCaccount) {
	if len(accounts) == 0 {
		fmt.Println("[DRY-TEST] No accounts to test")
		return
	}

	for _, account := range accounts {
		if account.Type != mc.Ms {
			continue
		}

		fmt.Printf("[DRY-TEST] Testing %s...", account.Email)

		err := account.MicrosoftAuthenticate("")
		if err != nil {
			fmt.Printf(" FAIL: %v\n", err)
		} else {
			fmt.Println(" PASS")
		}
	}
}

func testVPNConnections(rotator *vpn.Rotator) {
	if rotator == nil {
		vpnRegions, err := vpn.LoadRegions("vpn.txt")
		if err != nil || len(vpnRegions) == 0 {
			fmt.Println("[DRY-TEST] VPN: no regions configured")
			return
		}

		vpnConfig, _ := vpn.LoadConfig("vpn_config.txt")
		rotator, err = vpn.NewRotator(vpnRegions, vpnConfig)
		if err != nil {
			fmt.Printf("[DRY-TEST] VPN: failed to create rotator: %v\n", err)
			return
		}
	}

	vpnAuth, authErr := vpn.LoadAuth("vpn_auth.txt")
	if authErr == nil && vpnAuth.MullvadAccount != "" {
		mullvad := vpn.NewMullvadProvider()
		if err := mullvad.Authenticate(vpnAuth.MullvadAccount); err != nil {
			fmt.Printf("[DRY-TEST] Mullvad auth: FAIL: %v\n", err)
		} else {
			fmt.Println("[DRY-TEST] Mullvad auth: PASS")
		}
	}

	if authErr == nil && vpnAuth.ProtonEmail != "" && vpnAuth.ProtonPassword != "" {
		proton := vpn.NewProtonProvider()
		if err := proton.Authenticate(vpnAuth.ProtonEmail, vpnAuth.ProtonPassword); err != nil {
			fmt.Printf("[DRY-TEST] Proton auth: FAIL: %v\n", err)
		} else {
			fmt.Println("[DRY-TEST] Proton auth: PASS")
		}
	}

	err := rotator.Connect()
	if err != nil {
		fmt.Printf("[DRY-TEST] VPN connect: FAIL: %v\n", err)
		rotator.Disconnect()
		return
	}

	region := rotator.CurrentRegion()
	if region.Provider != "" {
		fmt.Printf("[DRY-TEST] VPN connect: PASS (%s:%s)\n", region.Provider, region.Country)
	} else {
		fmt.Println("[DRY-TEST] VPN connect: PASS")
	}

	shouldRotate := rotator.ShouldRotate()
	rotator.Rotate()
	fmt.Printf("[DRY-TEST] VPN rotation: shouldRotate=%v, rotated\n", shouldRotate)

	rotator.Disconnect()
	fmt.Println("[DRY-TEST] VPN: PASS")
}
