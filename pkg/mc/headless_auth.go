package mc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

type TokenData struct {
	Email        string    `json:"email"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Bearer       string    `json:"bearer"`
	ExpiresAt    time.Time `json:"expires_at"`
}

const (
	msLoginURL = "https://login.live.com/oauth20_authorize.srf?client_id=000000004C12AE6F&redirect_uri=https://login.live.com/oauth20_desktop.srf&scope=service::user.auth.xboxlive.com::MBI_SSL&response_type=token&locale=en"
	msTokenURL = "https://login.live.com/oauth20_token.srf"
	clientID   = "000000004C12AE6F"
)

func LoadTokensMap(path string) map[string]TokenData {
	tokens := make(map[string]TokenData)
	data, err := os.ReadFile(path)
	if err != nil {
		return tokens
	}
	json.Unmarshal(data, &tokens)
	return tokens
}

func SaveTokensMap(path string, tokens map[string]TokenData) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	// Ensure directory exists
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.MkdirAll(dir, 0755)
	}
	return os.WriteFile(path, data, 0644)
}

func (acc *MCaccount) HeadlessAuthenticate(headless bool) error {
	fmt.Printf("[*] auth: Headless authenticating %s...\n", acc.Email)

	accessToken, refreshToken, err := DoPlaywrightLogin(acc.Email, acc.Password, headless)
	if err != nil {
		return err
	}

	bearer, err := ExchangeForBearer(accessToken)
	if err != nil {
		return err
	}

	acc.Bearer = bearer
	acc.RefreshToken = refreshToken

	// Save to map
	if acc.TokensPath != "" {
		tokens := LoadTokensMap(acc.TokensPath)
		tokens[acc.Email] = TokenData{
			Email:        acc.Email,
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			Bearer:       bearer,
			ExpiresAt:    time.Now().Add(24 * time.Hour),
		}
		SaveTokensMap(acc.TokensPath, tokens)
	}

	return nil
}

func (acc *MCaccount) RefreshAuthenticate() error {
	if acc.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	fmt.Printf("[*] auth: Refreshing token for %s...\n", acc.Email)

	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("grant_type", "refresh_token")
	data.Set("scope", "service::user.auth.xboxlive.com::MBI_SSL")
	data.Set("refresh_token", acc.RefreshToken)

	resp, err := http.PostForm(msTokenURL, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("refresh failed: %s", string(body))
	}

	var res struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	json.Unmarshal(body, &res)

	bearer, err := ExchangeForBearer(res.AccessToken)
	if err != nil {
		return err
	}

	acc.Bearer = bearer
	acc.RefreshToken = res.RefreshToken

	// Save to map
	if acc.TokensPath != "" {
		tokens := LoadTokensMap(acc.TokensPath)
		tokens[acc.Email] = TokenData{
			Email:        acc.Email,
			AccessToken:  res.AccessToken,
			RefreshToken: res.RefreshToken,
			Bearer:       bearer,
			ExpiresAt:    time.Now().Add(24 * time.Hour),
		}
		SaveTokensMap(acc.TokensPath, tokens)
	}

	return nil
}

func DoPlaywrightLogin(email, password string, headless bool) (string, string, error) {
	pw, err := playwright.Run()
	if err != nil {
		return "", "", err
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(headless),
		Args: []string{
			"--no-sandbox",
			"--disable-setuid-sandbox",
			"--disable-blink-features=AutomationControlled",
			"--disable-infobars",
			"--window-size=1920,1080",
		},
	})
	if err != nil {
		return "", "", err
	}
	defer browser.Close()

	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
		Viewport: &playwright.Size{
			Width:  1920,
			Height: 1080,
		},
	})
	if err != nil {
		return "", "", err
	}
	defer context.Close()

	page, err := context.NewPage()
	if err != nil {
		return "", "", err
	}

	// Mask webdriver
	if err := page.AddInitScript(playwright.Script{
		Content: playwright.String(`Object.defineProperty(navigator, 'webdriver', {get: () => undefined})`),
	}); err != nil {
		return "", "", err
	}

	if _, err = page.Goto(msLoginURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		return "", "", err
	}

	emailSelectors := []string{"input[name='loginfmt']", "#i0116", "#usernameEntry", "input[type='email']"}
	var emailSelector string
	for _, s := range emailSelectors {
		if visible, _ := page.Locator(s).IsVisible(); visible {
			emailSelector = s
			break
		}
	}
	if emailSelector == "" {
		if _, err = page.WaitForSelector("input[type='email']", playwright.PageWaitForSelectorOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(10000),
		}); err == nil {
			emailSelector = "input[type='email']"
		}
	}
	if emailSelector == "" {
		return "", "", fmt.Errorf("email field not found")
	}
	page.Fill(emailSelector, email)
	time.Sleep(500 * time.Millisecond)
	page.Keyboard().Press("Enter")
	time.Sleep(1 * time.Second)

	passwordSelectors := []string{"input[name='passwd']", "#i0118", "#passwordEntry", "input[type='password']"}
	foundPass := false

	for i := 0; i < 20; i++ {
		title, _ := page.Title()
		currUrl := page.URL()
		frames := page.Frames()
		fmt.Printf("[*] auth: Poll %d: Title: %s, URL: %s, Frames: %d\n", i, title, currUrl, len(frames))

		for f_idx, f := range frames {
			f_title, _ := f.Title()
			f_url := f.URL()
			
			// Try to find password field
			for _, s := range passwordSelectors {
				if visible, _ := f.Locator(s).IsVisible(); visible {
					fmt.Printf("[*] auth: Found password selector in frame %d: %s\n", f_idx, s)
					f.Fill(s, password)
					time.Sleep(500 * time.Millisecond)
					f.Press(s, "Enter")
					foundPass = true
					break
				}
			}
			if foundPass {
				break
			}

			// If not found pass, look for "Use password" button
			if strings.Contains(strings.ToLower(f_title), "another way") || 
			   strings.Contains(strings.ToLower(title), "another way") ||
			   strings.Contains(strings.ToLower(f_url), "anotherway") ||
			   strings.Contains(strings.ToLower(currUrl), "anotherway") {
				
				btns, _ := f.QuerySelectorAll("button, a, [role='button']")
				for _, btn := range btns {
					text, _ := btn.InnerText()
					if strings.Contains(strings.ToLower(text), "password") {
						fmt.Printf("[*] auth: Found 'password' button in frame %d: %s. Clicking...\n", f_idx, text)
						btn.Click()
						break
					}
				}
			}
		}
		if foundPass {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if !foundPass {
		return "", "", fmt.Errorf("password field not found")
	}

	timeout := time.After(60 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timeout:
			absPath, _ := filepath.Abs("error_timeout.png")
			page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(absPath)})
			currUrl := page.URL()
			return "", "", fmt.Errorf("timeout waiting for tokens, last URL: %s (screenshot: %s)", currUrl, absPath)
		case <-ticker.C:
			u := page.URL()
			fmt.Printf("[*] auth: Waiting loop URL: %s\n", u)
			if strings.Contains(u, "access_token") && strings.Contains(u, "refresh_token") {
				parsed, _ := url.Parse(u)
				params, _ := url.ParseQuery(parsed.Fragment)
				return params.Get("access_token"), params.Get("refresh_token"), nil
			}
			for _, f := range page.Frames() {
				// Check for error messages in frames
				if exists, _ := f.Locator("#passwordError").IsVisible(); exists {
					errText, _ := f.Locator("#passwordError").InnerText()
					return "", "", fmt.Errorf("microsoft error: %s", errText)
				}
				if exists, _ := f.Locator("#usernameError").IsVisible(); exists {
					errText, _ := f.Locator("#usernameError").InnerText()
					return "", "", fmt.Errorf("microsoft error: %s", errText)
				}
				
				// New: check for specific rate limit/error text anywhere in frame
				f_content, _ := f.InnerText("body")
				if strings.Contains(f_content, "too many times") || strings.Contains(f_content, "incorrect") {
					return "", "", fmt.Errorf("microsoft error: %s", strings.TrimSpace(f_content))
				}
				// Generic error message detection
				errorSelectors := []string{".error", "[id*='Error']", ".alert-danger"}
				for _, es := range errorSelectors {
					if exists, _ := f.Locator(es).IsVisible(); exists {
						errText, _ := f.Locator(es).InnerText()
						if len(errText) > 0 {
							return "", "", fmt.Errorf("detected error in page: %s", errText)
						}
					}
				}

				btns, _ := f.QuerySelectorAll("button, input[type='submit'], [role='button']")
				for _, b := range btns {
					id, _ := b.GetAttribute("id")
					txt, _ := b.InnerText()
					val, _ := b.GetAttribute("value")
					
					lowerT := strings.ToLower(txt)
					lowerV := strings.ToLower(val)

					if id == "idSIButton9" || strings.Contains(lowerT, "yes") || strings.Contains(lowerT, "stay") || strings.Contains(lowerV, "yes") {
						fmt.Printf("[*] auth: Clicking suspected interstitial button: %s / %s (ID=%s)\n", txt, val, id)
						b.Click()
						goto next
					}
					if id == "iShowSkip" || strings.Contains(lowerT, "skip") {
						fmt.Printf("[*] auth: Clicking skip button: %s / %s (ID=%s)\n", txt, val, id)
						b.Click()
						goto next
					}
				}
			}
		next:
		}
	}
}

func ExchangeForBearer(msAccessToken string) (string, error) {
	// Re-using types from msa.go
	xblBody := xBLSignInBody{}
	xblBody.Properties.Authmethod = "RPS"
	xblBody.Properties.Sitename = "user.auth.xboxlive.com"
	xblBody.Properties.Rpsticket = msAccessToken
	xblBody.Relyingparty = "http://auth.xboxlive.com"
	xblBody.Tokentype = "JWT"
	encodedBody, _ := json.Marshal(xblBody)

	req, _ := http.NewRequest("POST", "https://user.auth.xboxlive.com/user/authenticate", bytes.NewReader(encodedBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-xbl-contract-version", "1")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var xblResp XBLSignInResp
	json.Unmarshal(respBody, &xblResp)
	if len(xblResp.Displayclaims.Xui) == 0 {
		return "", fmt.Errorf("XBL response invalid: %s", string(respBody))
	}
	uhs := xblResp.Displayclaims.Xui[0].Uhs
	xblToken := xblResp.Token

	xstsReqBody := xSTSPostBody{}
	xstsReqBody.Properties.Sandboxid = "RETAIL"
	xstsReqBody.Properties.Usertokens = []string{xblToken}
	xstsReqBody.Relyingparty = "rp://api.minecraftservices.com/"
	xstsReqBody.Tokentype = "JWT"
	encodedXSTS, _ := json.Marshal(xstsReqBody)

	req, _ = http.NewRequest("POST", "https://xsts.auth.xboxlive.com/xsts/authorize", bytes.NewReader(encodedXSTS))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	respBody, _ = io.ReadAll(resp.Body)

	var xstsRespObj xSTSAuthorizeResponse
	json.Unmarshal(respBody, &xstsRespObj)
	xstsToken := xstsRespObj.Token

	mojangBody := map[string]interface{}{
		"identityToken":       "XBL3.0 x=" + uhs + ";" + xstsToken,
		"ensureLegacyEnabled": true,
	}
	mojangEncoded, _ := json.Marshal(mojangBody)
	req, _ = http.NewRequest("POST", "https://api.minecraftservices.com/authentication/login_with_xbox", bytes.NewReader(mojangEncoded))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	respBody, _ = io.ReadAll(resp.Body)

	var bearerResp msGetMojangBearerResponse
	json.Unmarshal(respBody, &bearerResp)
	return bearerResp.AccessToken, nil
}
