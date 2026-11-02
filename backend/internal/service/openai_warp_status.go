package service

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	httppool "github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
)

const openAIWarpTraceURL = "https://www.cloudflare.com/cdn-cgi/trace"

type OpenAIWarpStatus struct {
	Connected bool      `json:"connected"`
	IP        string    `json:"ip"`
	CheckedAt time.Time `json:"checked_at"`
}

// GetOpenAIWarpStatus checks the actual WARP exit through the same SOCKS proxy
// used by OpenAI account requests. A failed probe never falls back to direct.
func GetOpenAIWarpStatus(ctx context.Context) OpenAIWarpStatus {
	client, err := httppool.GetClient(httppool.Options{ProxyURL: OpenAIWarpProxyURL(), Timeout: 8 * time.Second})
	if err != nil {
		return OpenAIWarpStatus{CheckedAt: time.Now()}
	}
	return probeOpenAIWarpStatus(ctx, client, openAIWarpTraceURL)
}

func probeOpenAIWarpStatus(ctx context.Context, client *http.Client, endpoint string) OpenAIWarpStatus {
	result := OpenAIWarpStatus{CheckedAt: time.Now()}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return result
	}
	resp, err := client.Do(req)
	if err != nil {
		return result
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result
	}
	var ip, warp string
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 4096))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		switch key {
		case "ip":
			ip = strings.TrimSpace(value)
		case "warp":
			warp = strings.TrimSpace(value)
		}
	}
	if scanner.Err() != nil || (warp != "on" && warp != "plus") || net.ParseIP(ip) == nil {
		return result
	}
	result.Connected = true
	result.IP = ip
	return result
}
