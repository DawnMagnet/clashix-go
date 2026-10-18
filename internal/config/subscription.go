package config

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// SubscriptionUserAgent is the user agent used by Clashix when fetching
	// subscription documents.
	SubscriptionUserAgent = "clash-verge/v2.4.3"
	// MaxSubscriptionSize is the maximum accepted subscription body size.
	MaxSubscriptionSize int64 = 16 << 20
	// DefaultSubscriptionTimeout bounds a complete subscription request.
	DefaultSubscriptionTimeout = 30 * time.Second
)

// Fetcher retrieves subscription configuration documents. Its zero value is
// usable and fetches through a client that ignores proxy environment
// variables.
type Fetcher struct {
	Client    *http.Client
	UserAgent string
	MaxSize   int64
}

// NewFetcher returns a Fetcher suitable for subscription downloads.
func NewFetcher() *Fetcher {
	return &Fetcher{
		Client:    NewHTTPClient(DefaultSubscriptionTimeout),
		UserAgent: SubscriptionUserAgent,
		MaxSize:   MaxSubscriptionSize,
	}
}

// NewHTTPClient returns an HTTP client that never uses proxy environment
// variables.
func NewHTTPClient(timeout time.Duration) *http.Client {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		transport = transport.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

// Fetch downloads a subscription using a default Fetcher.
func Fetch(ctx context.Context, url string) (map[string]any, error) {
	return NewFetcher().Fetch(ctx, url)
}

// Fetch downloads a subscription and parses either a YAML mapping or a base64
// encoded YAML mapping.
func (f *Fetcher) Fetch(ctx context.Context, url string) (map[string]any, error) {
	client := f.Client
	if client == nil {
		client = NewHTTPClient(DefaultSubscriptionTimeout)
	}
	userAgent := f.UserAgent
	if userAgent == "" {
		userAgent = SubscriptionUserAgent
	}
	maxSize := f.MaxSize
	if maxSize <= 0 {
		maxSize = MaxSubscriptionSize
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create subscription request for %q: %w", url, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/yaml, text/yaml, text/plain, */*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch subscription %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch subscription %q: unexpected HTTP status %s", url, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read subscription %q: %w", url, err)
	}
	if int64(len(body)) > maxSize {
		return nil, fmt.Errorf("subscription %q exceeds %d bytes", url, maxSize)
	}

	cfg, err := parseSubscription(body)
	if err != nil {
		return nil, fmt.Errorf("parse subscription %q: %w", url, err)
	}
	return cfg, nil
}

func parseSubscription(body []byte) (map[string]any, error) {
	body = bytes.TrimPrefix(bytes.TrimSpace(body), []byte{0xEF, 0xBB, 0xBF})
	if len(body) == 0 {
		return nil, fmt.Errorf("subscription body is empty")
	}

	cfg, yamlErr := Parse(body)
	if yamlErr == nil {
		return cfg, nil
	}

	decoded, err := decodeBase64(body)
	if err != nil {
		return nil, fmt.Errorf("body is not a YAML mapping (%v) and is not base64: %w", yamlErr, err)
	}
	cfg, err = Parse(decoded)
	if err != nil {
		return nil, fmt.Errorf("decoded body is not a YAML mapping: %w", err)
	}
	return cfg, nil
}

func decodeBase64(data []byte) ([]byte, error) {
	compact := make([]byte, 0, len(data))
	for _, b := range data {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			compact = append(compact, b)
		}
	}
	if len(compact) == 0 {
		return nil, fmt.Errorf("empty base64 body")
	}

	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(string(compact))
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("invalid base64: %w", lastErr)
}
