package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Kqzz/MCsniperGO/pkg/mc"
)

func main() {
	var (
		creds       string
		tokensPath  string
		headless    bool
		daemon      bool
		credsFile   string
	)
	flag.StringVar(&creds, "creds", "", "Credentials in format email:password,email2:password2")
	flag.StringVar(&credsFile, "creds-file", "", "Path to a JSON file containing {email: password}")
	flag.StringVar(&tokensPath, "tokens", "tokens.json", "Path to tokens.json")
	flag.BoolVar(&headless, "headless", true, "Run browser in headless mode")
	flag.BoolVar(&daemon, "daemon", false, "Run in a loop every 24 hours")
	flag.Parse()

	if credsFile != "" {
		data, err := os.ReadFile(credsFile)
		if err == nil {
			var credsMap map[string]string
			if err := json.Unmarshal(data, &credsMap); err == nil {
				var parts []string
				for k, v := range credsMap {
					parts = append(parts, fmt.Sprintf("%s:%s", k, v))
				}
				creds = strings.Join(parts, ",")
			}
		}
	}

	if creds == "" {
		creds = os.Getenv("MS_ACCOUNTS")
	}

	if creds == "" {
		fmt.Println("Usage: auth-refresher -creds email:password,email2:password2 [-tokens tokens.json] [-daemon]")
		os.Exit(1)
	}

	rand.Seed(time.Now().UnixNano())

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	
	accountConfigs := strings.Split(creds, ",")
	
	if daemon {
		fmt.Printf("[*] Daemon mode started. Refreshing %d accounts every 24h...\n", len(accountConfigs))
		ticker := time.NewTicker(24 * time.Hour)
		
		runAllRefreshes(accountConfigs, tokensPath, headless)
		
		for {
			select {
			case <-ticker.C:
				runAllRefreshes(accountConfigs, tokensPath, headless)
			case <-c:
				fmt.Println("\nInterrupted, exiting...")
				os.Exit(0)
			}
		}
	} else {
		runAllRefreshes(accountConfigs, tokensPath, headless)
	}
}

func runAllRefreshes(accountConfigs []string, tokensPath string, headless bool) {
	tokensMap := mc.LoadTokensMap(tokensPath)
	
	for _, config := range accountConfigs {
		parts := strings.SplitN(config, ":", 2)
		if len(parts) != 2 {
			fmt.Printf("[!] Invalid config: %s\n", config)
			continue
		}
		
		email := parts[0]
		password := parts[1]
		
		fmt.Printf("[%s] Processing %s...\n", time.Now().Format("15:04:05"), email)
		
		acc := &mc.MCaccount{
			Email:      email,
			Password:   password,
			TokensPath: tokensPath,
		}
		
		if td, ok := tokensMap[email]; ok {
			acc.RefreshToken = td.RefreshToken
		}

		var err error
		if acc.RefreshToken != "" {
			fmt.Println("[*] Attempting refresh using refresh_token...")
			err = acc.RefreshAuthenticate()
			if err != nil {
				fmt.Printf("[!] Refresh failed: %v. Falling back to headless...\n", err)
				err = acc.HeadlessAuthenticate(headless)
			}
		} else {
			err = acc.HeadlessAuthenticate(headless)
		}

		if err != nil {
			fmt.Printf("[!] Authentication failed for %s: %v\n", email, err)
			continue
		}

		fmt.Printf("[*] Success for %s! Bearer token length: %d\n", email, len(acc.Bearer))
	}
}
