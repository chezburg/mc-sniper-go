package droptime

import (
	"fmt"
	"io"
	"net/http"
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

func FetchDroptimes() ([]DropInfo, error) {
	fmt.Println("[*] Fetching droptimes from 3name.xyz...")

	req, err := http.NewRequest("GET", "https://3name.xyz/list", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	html := string(body)

	nameRegex := regexp.MustCompile(`/name/([a-zA-Z0-9_]+)`)
	matches := nameRegex.FindAllStringSubmatch(html, -1)

	seen := make(map[string]bool)
	var names []string
	for _, match := range matches {
		if len(match) > 1 {
			name := match[1]
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}

	fmt.Printf("[*] Found %d names from 3name.xyz/list\n", len(names))

	var drops []DropInfo
	for i, name := range names {
		if i > 0 {
			time.Sleep(1 * time.Second)
		}

		dropInfo, err := FetchDropInfo(name)
		if err != nil {
			fmt.Printf("[*] Failed to fetch drop info for %s: %v\n", name, err)
			continue
		}

		drops = append(drops, dropInfo)
	}

	sort.Slice(drops, func(i, j int) bool {
		return drops[i].DropStart.Before(drops[j].DropStart)
	})

	fmt.Printf("[*] Collected %d droptimes\n", len(drops))

	return drops, nil
}

func FetchDropInfo(username string) (DropInfo, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://3name.xyz/name/%s", username), nil)
	if err != nil {
		return DropInfo{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return DropInfo{}, fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return DropInfo{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return DropInfo{}, fmt.Errorf("failed to read response: %w", err)
	}

	html := string(body)

	lowerRegex := regexp.MustCompile(`data-lower-bound="(\d+)"`)
	upperRegex := regexp.MustCompile(`data-upper-bound="(\d+)"`)

	lowerMatch := lowerRegex.FindStringSubmatch(html)
	upperMatch := upperRegex.FindStringSubmatch(html)

	if len(lowerMatch) < 2 || len(upperMatch) < 2 {
		return DropInfo{}, fmt.Errorf("could not find drop timestamps for %s", username)
	}

	lowerBound, err := strconv.ParseInt(lowerMatch[1], 10, 64)
	if err != nil {
		return DropInfo{}, fmt.Errorf("failed to parse lower bound: %w", err)
	}

	upperBound, err := strconv.ParseInt(upperMatch[1], 10, 64)
	if err != nil {
		return DropInfo{}, fmt.Errorf("failed to parse upper bound: %w", err)
	}

	return DropInfo{
		Username:  username,
		DropStart: time.Unix(lowerBound/1000, 0),
		DropEnd:   time.Unix(upperBound/1000, 0),
	}, nil
}