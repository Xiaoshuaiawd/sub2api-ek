package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	_ "golang.org/x/image/webp"
)

var ErrIntelligenceOCR = errors.New("image text recognition failed; please retry")
var intelligenceOCRSlots = make(chan struct{}, 2)
var intelligenceBlockedIPRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // shared address space
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64 can embed a private IPv4 address
	netip.MustParsePrefix("64:ff9b:1::/48"), // local-use NAT64
	netip.MustParsePrefix("2002::/16"),      // 6to4
	netip.MustParsePrefix("2001::/32"),      // Teredo
}

func intelligencePublicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range intelligenceBlockedIPRanges {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

// IntelligenceRouteWithOCR examines only the latest user input and fails
// closed if an image cannot be read or OCR fails. Empty OCR is ordinary input.
func IntelligenceRouteWithOCR(ctx context.Context, body []byte) (bool, error) {
	return intelligenceRouteWithOCR(ctx, body, recognizeIntelligenceImage)
}

func intelligenceRouteWithOCR(ctx context.Context, body []byte, recognize func(context.Context, string) (string, error)) (bool, error) {
	images, err := ExtractIntelligenceLatestImages(body)
	if err != nil {
		return false, ErrIntelligenceOCR
	}
	if len(images) == 0 {
		return IsIntelligenceTestRequest(body), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	select {
	case intelligenceOCRSlots <- struct{}{}:
		defer func() { <-intelligenceOCRSlots }()
	case <-ctx.Done():
		return false, ErrIntelligenceOCR
	}
	var request map[string]any
	_ = json.Unmarshal(body, &request)
	text := intelligenceLatestUserText(request)
	for _, image := range images {
		part, err := recognize(ctx, image)
		if err != nil {
			return false, ErrIntelligenceOCR
		}
		text += " " + part
	}
	text = normalizeIntelligenceVisualText(text)
	compact := strings.Join(strings.Fields(text), "")
	return matchesIntelligencePrompt(text) || matchesVisualIntelligencePrompt(text) || matchesIntelligencePrompt(compact) || matchesVisualIntelligencePrompt(compact), nil
}

var intelligenceImageClient = &http.Client{
	Timeout: 12 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 || req.URL.User != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
			return ErrIntelligenceOCR
		}
		return nil
	},
	Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || urlvalidator.IsBlockedHost(host) {
			return nil, ErrIntelligenceOCR
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, ErrIntelligenceOCR
		}
		for _, ip := range ips {
			if !intelligencePublicIP(ip.IP) {
				return nil, ErrIntelligenceOCR
			}
		}
		dialer := net.Dialer{Timeout: 5 * time.Second}
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				break
			}
		}
		return nil, ErrIntelligenceOCR
	}},
}

func recognizeIntelligenceImage(ctx context.Context, ref string) (string, error) {
	var data []byte
	if strings.HasPrefix(strings.ToLower(ref), "data:") {
		_, payload, ok := strings.Cut(ref, ",")
		if !ok {
			return "", ErrIntelligenceOCR
		}
		var err error
		data, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return "", ErrIntelligenceOCR
		}
	} else {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
		if err != nil {
			return "", ErrIntelligenceOCR
		}
		resp, err := intelligenceImageClient.Do(req)
		if err != nil {
			return "", ErrIntelligenceOCR
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", ErrIntelligenceOCR
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, intelligenceImageMaxBytes+1))
		if err != nil || len(data) > intelligenceImageMaxBytes {
			return "", ErrIntelligenceOCR
		}
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 20_000_000 {
		return "", ErrIntelligenceOCR
	}
	cmd := exec.CommandContext(ctx, "tesseract", "stdin", "stdout", "-l", "chi_sim+chi_tra+eng")
	cmd.Env = append(os.Environ(), "OMP_THREAD_LIMIT=1")
	cmd.Stdin = bytes.NewReader(data)
	output, err := cmd.Output()
	if err != nil || len(output) > 1<<20 {
		return "", ErrIntelligenceOCR
	}
	return string(output), nil
}
