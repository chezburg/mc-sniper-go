package mc

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"time"
)

var oauthDebugLogger *log.Logger

func init() {
	debugFile, err := os.OpenFile("auth_debug_oauth.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		oauthDebugLogger = log.New(os.Stdout, "[DEBUG OAUTH] ", log.LstdFlags)
	} else {
		oauthDebugLogger = log.New(debugFile, "[DEBUG OAUTH] ", log.LstdFlags)
	}
}

/*

Credit to emily (@impliedgg) for the entire oauth flow!
Client ID is 00000000441cc96b, Minecraft for Nintendo Switch

Flow is as follows:
POST https://login.live.com/oauth20_connect.srf
?client_id={client_id}
&scope=XboxLive.signin

Inform user to visit link from response.verification_uri and enter code response.user_code.

POST https://login.live.com/oauth20_token.srf
?grant_type=urn:ietf:params:oauth:grant-type:device_code
&client_id={client_id}
&device_code={respone.device_code}

once every response.interval seconds until expires_in timeout or successful poll.

Errors to properly handle in response.error:
authorization_pending - keep waiting. user isn't done.
authorization_declined - user declined auth, fail to authenticate.
bad_verification_code - this one should request a bug report on github. won't happen normally
expired_token - stop polling, fail to authenticate. user took too long.const

Fields to use once response.error is nil:
access_token - use this with https://user.auth.xboxlive.com/user/authenticate to get xsts done.
expires_in - if implemented, should request reauthentication once expired.

*/

// we only take the useful fields here.

type msDeviceInitResponse struct {
	VerificationURI string `json:"verification_uri"`
	UserCode        string `json:"user_code"`
	Message         string `json:"message"`
	Interval        int    `json:"interval"`
	DeviceCode      string `json:"device_code"`
}

type msErrorPollResponse struct {
	Error string `json:"error"`
}

type msSuccessPollResponse struct {
	AccessToken string `json:"access_token"`
}

const client_id = "00000000441cc96b"

// types in msa.go are used here as well.

func (account *MCaccount) OauthFlow() error {
	oauthDebugLogger.Println("=== Starting OauthFlow ===")

	jar, err := cookiejar.New(nil)
	if err != nil {
		oauthDebugLogger.Printf("ERROR creating cookiejar: %v", err)
		return err
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			Renegotiation:      tls.RenegotiateOnceAsClient,
			InsecureSkipVerify: true},
	}

	client := &http.Client{
		Jar:       jar,
		Transport: tr,
	}

	reqParams := fmt.Sprintf("client_id=%s&scope=XboxLive.signin&response_type=device_code", client_id)

	oauthDebugLogger.Printf("POST https://login.live.com/oauth20_connect.srf with body: %s", reqParams)

	req, _ := http.NewRequest("POST", "https://login.live.com/oauth20_connect.srf", bytes.NewBuffer([]byte(reqParams)))

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		oauthDebugLogger.Printf("ERROR: %v", err)
		return err
	}
	defer resp.Body.Close()
	respbytes, err := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		oauthDebugLogger.Printf("ERROR: non-200 status on devicecode post: HTTP %d, body: %s", resp.StatusCode, string(respbytes))
		return errors.New("non-200 status on devicecode post")
	}

	if err != nil {
		oauthDebugLogger.Printf("ERROR reading response: %v", err)
		return err
	}

	oauthDebugLogger.Printf("Device code response: %s", string(respbytes))

	var respObj msDeviceInitResponse
	err = json.Unmarshal(respbytes, &respObj)
	if err != nil {
		oauthDebugLogger.Printf("ERROR unmarshaling device code response: %v", err)
		return err
	}

	oauthDebugLogger.Printf("Device code parsed: verification_uri=%s, user_code=%s, interval=%d", respObj.VerificationURI, respObj.UserCode, respObj.Interval)

	fmt.Printf("[*] auth: Please visit %v and use the code %v to continue\n", respObj.VerificationURI, respObj.UserCode)

	return pollEndpoint(account, respObj.DeviceCode, respObj.Interval)
}

func authWithToken(account *MCaccount, access_token_from_ms string) error {
	oauthDebugLogger.Println("=== Starting authWithToken ===")
	oauthDebugLogger.Printf("MS Access Token: %s...", access_token_from_ms[:min(50, len(access_token_from_ms))])

	jar, err := cookiejar.New(nil)
	if err != nil {
		oauthDebugLogger.Printf("ERROR creating cookiejar: %v", err)
		return err
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			Renegotiation:      tls.RenegotiateOnceAsClient,
			InsecureSkipVerify: true,
		},
	}

	client := &http.Client{
		Jar:       jar,
		Transport: tr,
	}

	// === XBL Authentication ===
	oauthDebugLogger.Println("=== XBL Authentication ===")
	data := xBLSignInBody{
		Properties: struct {
			Authmethod string "json:\"AuthMethod\""
			Sitename   string "json:\"SiteName\""
			Rpsticket  string "json:\"RpsTicket\""
		}{
			Authmethod: "RPS",
			Sitename:   "user.auth.xboxlive.com",
			Rpsticket:  "d=" + access_token_from_ms,
		},
		Relyingparty: "http://auth.xboxlive.com",
		Tokentype:    "JWT",
	}

	encodedBody, err := json.Marshal(data)
	if err != nil {
		oauthDebugLogger.Printf("ERROR marshaling XBL body: %v", err)
		return err
	}
	oauthDebugLogger.Printf("XBL request body: %s", string(encodedBody))

	req, err := http.NewRequest("POST", "https://user.auth.xboxlive.com/user/authenticate", bytes.NewReader(encodedBody))
	if err != nil {
		oauthDebugLogger.Printf("ERROR creating XBL request: %v", err)
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-xbl-contract-version", "1")

	oauthDebugLogger.Println("POST https://user.auth.xboxlive.com/user/authenticate")

	resp, err := client.Do(req)
	if err != nil {
		oauthDebugLogger.Printf("ERROR XBL request: %v", err)
		return err
	}

	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if resp.StatusCode == 400 {
		oauthDebugLogger.Printf("XBL 400 - invalid Rpsticket")
		return errors.New("invalid Rpsticket field probably")
	}

	if err != nil {
		oauthDebugLogger.Printf("ERROR reading XBL response: %v", err)
		return err
	}

	oauthDebugLogger.Printf("XBL response HTTP %d, body: %s", resp.StatusCode, string(respBodyBytes))

	var respBody XBLSignInResp

	json.Unmarshal(respBodyBytes, &respBody)

	uhs := respBody.Displayclaims.Xui[0].Uhs
	XBLToken := respBody.Token

	oauthDebugLogger.Printf("XBL success! uhs=%s, token length=%d", uhs, len(XBLToken))

	// === XSTS Authentication ===
	oauthDebugLogger.Println("=== XSTS Authentication ===")

	xstsBody := xSTSPostBody{
		Properties: struct {
			Sandboxid  string   "json:\"SandboxId\""
			Usertokens []string "json:\"UserTokens\""
		}{
			Sandboxid: "RETAIL",
			Usertokens: []string{
				XBLToken,
			},
		},
		Relyingparty: "rp://api.minecraftservices.com/",
		Tokentype:    "JWT",
	}

	encodedXstsBody, err := json.Marshal(xstsBody)
	if err != nil {
		oauthDebugLogger.Printf("ERROR marshaling XSTS body: %v", err)
		return err
	}
	oauthDebugLogger.Printf("XSTS request body: %s", string(encodedXstsBody))

	req, err = http.NewRequest("POST", "https://xsts.auth.xboxlive.com/xsts/authorize", bytes.NewReader(encodedXstsBody))
	if err != nil {
		oauthDebugLogger.Printf("ERROR creating XSTS request: %v", err)
		return err
	}

	oauthDebugLogger.Println("POST https://xsts.auth.xboxlive.com/xsts/authorize")

	resp, err = client.Do(req)

	if err != nil {
		oauthDebugLogger.Printf("ERROR XSTS request: %v", err)
		return err
	}

	respBodyBytes, err = io.ReadAll(resp.Body)

	if err != nil {
		oauthDebugLogger.Printf("ERROR reading XSTS response: %v", err)
		return err
	}

	oauthDebugLogger.Printf("XSTS response HTTP %d, body: %s", resp.StatusCode, string(respBodyBytes))

	if resp.StatusCode == 401 {
		var authorizeXstsFail xSTSAuthorizeResponseFail
		json.Unmarshal(respBodyBytes, &authorizeXstsFail)
		oauthDebugLogger.Printf("XSTS 401 error: xerr=%d, message=%s", authorizeXstsFail.Xerr, authorizeXstsFail.Message)
		switch authorizeXstsFail.Xerr {
		case 2148916238:
			{
				return errors.New("microsoft account belongs to someone under 18! add to family for this to work")
			}
		case 2148916233:
			{
				return errors.New("you have no xbox account! Sign up for one to continue")
			}
		default:
			{
				return fmt.Errorf("got error code %v when trying to authorize XSTS token", authorizeXstsFail.Xerr)
			}
		}
	}

	var xstsAuthorizeResp xSTSAuthorizeResponse
	json.Unmarshal(respBodyBytes, &xstsAuthorizeResp)

	xstsToken := xstsAuthorizeResp.Token

	oauthDebugLogger.Printf("XSTS success! token length=%d", len(xstsToken))

	// === Mojang Authentication ===
	oauthDebugLogger.Println("=== Mojang Authentication ===")

	mojangBearerBody := msGetMojangbearerBody{
		Identitytoken:       "XBL3.0 x=" + uhs + ";" + xstsToken,
		Ensurelegacyenabled: true,
	}

	mojangBearerBodyEncoded, err := json.Marshal(mojangBearerBody)

	if err != nil {
		oauthDebugLogger.Printf("ERROR marshaling Mojang body: %v", err)
		return err
	}

	oauthDebugLogger.Printf("Mojang request body: %s", string(mojangBearerBodyEncoded))

	req, err = http.NewRequest("POST", "https://api.minecraftservices.com/authentication/login_with_xbox", bytes.NewReader(mojangBearerBodyEncoded))

	req.Header.Set("Content-Type", "application/json")

	if err != nil {
		oauthDebugLogger.Printf("ERROR creating Mojang request: %v", err)
		return err
	}

	oauthDebugLogger.Println("POST https://api.minecraftservices.com/authentication/login_with_xbox")

	resp, err = client.Do(req)
	if err != nil {
		oauthDebugLogger.Printf("ERROR Mojang request: %v", err)
		return err
	}

	mcBearerResponseBytes, err := io.ReadAll(resp.Body)

	if err != nil {
		oauthDebugLogger.Printf("ERROR reading Mojang response: %v", err)
		return err
	}

	oauthDebugLogger.Printf("Mojang response HTTP %d, body: %s", resp.StatusCode, string(mcBearerResponseBytes))

	var mcBearerResp msGetMojangBearerResponse

	json.Unmarshal(mcBearerResponseBytes, &mcBearerResp)

	account.Bearer = mcBearerResp.AccessToken

	oauthDebugLogger.Printf("=== authWithToken SUCCESS! Bearer token length=%d ===", len(account.Bearer))

	return nil
}

func pollEndpoint(account *MCaccount, device_code string, interval int) error {

	oauthDebugLogger.Printf("=== Starting pollEndpoint (device_code=%s..., interval=%d) ===", device_code[:min(20, len(device_code))], interval)

	sleepDuration := time.Second * time.Duration(interval)
	jar, err := cookiejar.New(nil)
	if err != nil {
		oauthDebugLogger.Printf("ERROR creating cookiejar: %v", err)
		return err
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			Renegotiation:      tls.RenegotiateOnceAsClient,
			InsecureSkipVerify: true},
	}

	client := &http.Client{
		Jar:       jar,
		Transport: tr,
	}

	pollCount := 0
	reqParams := fmt.Sprintf("grant_type=urn:ietf:params:oauth:grant-type:device_code&device_code=%s&client_id=%s", device_code, client_id)
	for {
		pollCount++
		time.Sleep(sleepDuration)
		
		oauthDebugLogger.Printf("Poll attempt #%d", pollCount)

		req, err := http.NewRequest("POST", "https://login.live.com/oauth20_token.srf", bytes.NewBuffer([]byte(reqParams)))
		if err != nil {
			oauthDebugLogger.Printf("ERROR creating poll request: %v", err)
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		
		oauthDebugLogger.Println("POST https://login.live.com/oauth20_token.srf")
		
		resp, err := client.Do(req)
		if err != nil {
			oauthDebugLogger.Printf("ERROR poll request: %v", err)
			return err
		}
		defer resp.Body.Close()
		byteRes, err := io.ReadAll(resp.Body)
		if err != nil {
			oauthDebugLogger.Printf("ERROR reading poll response: %v", err)
			return err
		}

		oauthDebugLogger.Printf("Poll response HTTP %d, body: %s", resp.StatusCode, string(byteRes))

		if resp.StatusCode == 400 {
			var r msErrorPollResponse
			err = json.Unmarshal(byteRes, &r)
			if err != nil {
				oauthDebugLogger.Printf("ERROR unmarshaling poll error: %v", err)
				return err
			}
			oauthDebugLogger.Printf("Poll error: %s", r.Error)
			switch r.Error {
			case "authorization_pending":
				oauthDebugLogger.Println("Authorization pending, continuing to poll...")
				continue
			case "authorization_declined", "expired_token":
				oauthDebugLogger.Printf("Authorization failed: %s", r.Error)
				return errors.New("authorization failed. cannot continue")
			default:
				oauthDebugLogger.Printf("Unknown poll error: %s", r.Error)
				return errors.New("unknown state on 400 status")
			}
		} else if resp.StatusCode == 200 {
			var r msSuccessPollResponse
			err = json.Unmarshal(byteRes, &r)
			if err != nil {
				oauthDebugLogger.Printf("ERROR unmarshaling poll success: %v", err)
				return err
			}
			oauthDebugLogger.Printf("Poll success! access_token: %s...", r.AccessToken[:min(50, len(r.AccessToken))])
			return authWithToken(account, r.AccessToken)
		} else {
			oauthDebugLogger.Printf("Unexpected status code: %d", resp.StatusCode)
			return errors.New("status code response not 200 or 400")
		}
	}
}
