package droptime

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"time"
)

type DropInfo struct {
	Username  string
	DropStart time.Time
	DropEnd   time.Time
}

func testConnectivity() {
	fmt.Println("[*] Running connectivity diagnostic...")
	
	// DNS Check
	_, err := net.LookupHost("3name.xyz")
	if err != nil {
		fmt.Printf("[!] DNS Resolution for 3name.xyz failed: %v\n", err)
	} else {
		fmt.Println("[*] DNS Resolution: SUCCESS")
	}

	// Cloudflare Ping (Curl)
	cmd := exec.Command("curl", "-Is", "--connect-timeout", "10", "https://1.1.1.1")
	err = cmd.Run()
	if err != nil {
		fmt.Printf("[!] Connectivity test to 1.1.1.1 (Cloudflare) failed: %v\n", err)
	} else {
		fmt.Println("[*] Connectivity to 1.1.1.1: SUCCESS")
	}

	// 3name.xyz Ping (Curl)
	cmd = exec.Command("curl", "-Is", "--connect-timeout", "10", "-k", "https://3name.xyz")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[!] Connectivity test to 3name.xyz failed: %v (output: %s)\n", err, string(output))
	} else {
		fmt.Println("[*] Connectivity to 3name.xyz: SUCCESS")
	}
}

func curlFetch(url string, proxy string) (string, error) {
	args := []string{"-sS", "-L", "-k", "--connect-timeout", "30", "-m", "60", "-A", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", url}
	
	if proxy != "" {
		args = append([]string{"-x", proxy}, args...)
	}

	cmd := exec.Command("curl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("curl failed (proxy: %s): %w (output: %s)", proxy, err, string(output))
	}
	return string(output), nil
}

func FetchDroptimes(proxies []string) ([]DropInfo, error) {
	fmt.Println("[*] Fetching droptimes from 3name.xyz...")
	
	testConnectivity()

	var html string
	var err error

	// Try direct first
	html, err = curlFetch("https://3name.xyz/list", "")
	
	// If direct fails and we have proxies, try the first proxy
	if err != nil && len(proxies) > 0 && proxies[0] != "" {
		fmt.Printf("[*] Direct fetch failed, trying with proxy...\n")
		html, err = curlFetch("https://3name.xyz/list", proxies[0])
	}

	if err != nil {
		return nil, fmt.Errorf("failed to fetch list: %w", err)
	}

	// Regex to find username and lower bound together, handles newlines
	combinedRegex := regexp.MustCompile(`(?s)<a href="/name/([a-zA-Z0-9_]+)">.*?data-lower-bound="(\d+)"`)
	matches := combinedRegex.FindAllStringSubmatch(html, -1)

	if len(matches) == 0 {
		return nil, fmt.Errorf("no name matches found in the HTML - the website structure might have changed")
	}

	var drops []DropInfo
	seen := make(map[string]bool)

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		username := match[1]
		if seen[username] {
			continue
		}
		seen[username] = true

		lowerBound, err := strconv.ParseInt(match[2], 10, 64)
		if err != nil {
			continue
		}

		// Convert ms to seconds
		startTime := time.Unix(lowerBound/1000, 0)
		endTime := startTime.Add(60 * time.Second)

		drops = append(drops, DropInfo{
			Username:  username,
			DropStart: startTime,
			DropEnd:   endTime,
		})
	}

	sort.Slice(drops, func(i, j int) bool {
		return drops[i].DropStart.Before(drops[j].DropStart)
	})

	fmt.Printf("[*] Collected %d droptimes\n", len(drops))

	return drops, nil
}

func FetchDropInfo(username string, proxies []string) (DropInfo, error) {
	var html string
	var err error

	// Try direct first
	html, err = curlFetch(fmt.Sprintf("https://3name.xyz/name/%s", username), "")
	
	// If direct fails and we have proxies, try the first proxy
	if err != nil && len(proxies) > 0 && proxies[0] != "" {
		html, err = curlFetch(fmt.Sprintf("https://3name.xyz/name/%s", username), proxies[0])
	}

	if err != nil {
		return DropInfo{}, fmt.Errorf("failed to fetch: %w", err)
	}

	lowerRegex := regexp.MustCompile(`data-lower-bound="(\d+)"`)
	upperRegex := regexp.MustCompile(`data-upper-bound="(\d+)"`)

	lowerMatch := lowerRegex.FindStringSubmatch(html)
	upperMatch := upperRegex.FindStringSubmatch(html)

	if len(lowerMatch) < 2 {
		return DropInfo{}, fmt.Errorf("could not find drop timestamps (data-lower-bound) for %s", username)
	}

	lowerBound, err := strconv.ParseInt(lowerMatch[1], 10, 64)
	if err != nil {
		return DropInfo{}, fmt.Errorf("failed to parse lower bound: %w", err)
	}

	var upperBound int64
	if len(upperMatch) >= 2 {
		upperBound, _ = strconv.ParseInt(upperMatch[1], 10, 64)
	} else {
		upperBound = lowerBound + 60000 // 60s default
	}

	return DropInfo{
		Username:  username,
		DropStart: time.Unix(lowerBound/1000, 0),
		DropEnd:   time.Unix(upperBound/1000, 0),
	}, nil
}
