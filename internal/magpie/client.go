// Package magpie is a read-only client for Magpie Desktop's Gateway HTTP API.
// It never proxies model traffic or writes Magpie's configuration.
package magpie

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to one configured Gateway.
type Client struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
	userAgent  string
}

// ErrorKind classifies failures for UI recovery and bounded retry policy.
type ErrorKind string

const (
	ErrorNetwork  ErrorKind = "network"
	ErrorTimeout  ErrorKind = "timeout"
	ErrorAuth     ErrorKind = "auth"
	ErrorNotFound ErrorKind = "not-found"
	ErrorHTTP     ErrorKind = "http"
	ErrorProtocol ErrorKind = "protocol"
)

// APIError is a safe, structured error. It intentionally never stores the
// request body or the response body.
type APIError struct {
	Kind       ErrorKind
	StatusCode int
	Code       string
	Message    string
	RetryAfter time.Duration
	Err        error
}

func (e *APIError) Error() string {
	switch {
	case e.Message != "" && e.StatusCode != 0:
		return fmt.Sprintf("magpie: HTTP %d: %s", e.StatusCode, e.Message)
	case e.Message != "":
		return "magpie: " + e.Message
	case e.StatusCode != 0:
		return fmt.Sprintf("magpie: HTTP %d", e.StatusCode)
	case e.Err != nil:
		return "magpie: " + e.Err.Error()
	default:
		return "magpie: request failed"
	}
}

func (e *APIError) Unwrap() error { return e.Err }

// Options configures a Client.
type Options struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	// HTTPClient is optional; when set, its Transport is reused but Timeout
	// is applied by this client's context as well.
	HTTPClient *http.Client
}

// NewClient validates the address and builds the client.
func NewClient(opts Options) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("gateway base URL must be an absolute http:// or https:// URL")
	}
	if u.User != nil {
		return nil, errors.New("gateway base URL must not contain credentials")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}
	clone := *hc
	c := &Client{baseURL: u, apiKey: strings.TrimSpace(opts.APIKey), httpClient: &clone, userAgent: "magpie-native-routing/0.1"}
	c.httpClient.Timeout = timeout
	if clone.Transport == nil {
		c.httpClient.Transport = defaultTransport()
	}
	return c, nil
}

func defaultTransport() http.RoundTripper {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   4 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   4 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
}

// BaseURL is the normalized address, without a trailing slash.
func (c *Client) BaseURL() string { return c.baseURL.String() }

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	u := *c.baseURL
	u.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return &APIError{Kind: ErrorProtocol, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		kind := ErrorNetwork
		if errors.Is(err, context.DeadlineExceeded) {
			kind = ErrorTimeout
		}
		return &APIError{Kind: kind, Message: "gateway is unreachable", Err: err}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return readAPIError(res)
	}
	dec := json.NewDecoder(io.LimitReader(res.Body, 32<<20))
	if err := dec.Decode(out); err != nil {
		return &APIError{Kind: ErrorProtocol, StatusCode: res.StatusCode, Message: "invalid JSON response", Err: err}
	}
	return nil
}

// RawGET exposes a bounded read-only GET for optional diagnostics and
// future endpoints. It never sends a request body and never follows a
// redirect to a different origin.
func (c *Client) RawGET(ctx context.Context, path string, q url.Values, limit int64) ([]byte, error) {
	if limit <= 0 || limit > 32<<20 {
		limit = 1 << 20
	}
	u := *c.baseURL
	u.Path = strings.TrimRight(c.baseURL.Path, "/") + "/" + strings.TrimLeft(path, "/")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &APIError{Kind: ErrorProtocol, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		kind := ErrorNetwork
		if errors.Is(err, context.DeadlineExceeded) {
			kind = ErrorTimeout
		}
		return nil, &APIError{Kind: kind, Message: "gateway is unreachable", Err: err}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, readAPIError(res)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return nil, &APIError{Kind: ErrorProtocol, StatusCode: res.StatusCode, Message: "could not read gateway response", Err: err}
	}
	if !json.Valid(bytes.TrimSpace(b)) {
		return nil, &APIError{Kind: ErrorProtocol, StatusCode: res.StatusCode, Message: "invalid JSON response"}
	}
	return b, nil
}

func readAPIError(res *http.Response) error {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 256<<10))
	_ = json.Unmarshal(raw, &payload)
	code := ""
	switch v := payload.Error.Code.(type) {
	case string:
		code = v
	case float64:
		code = strconv.FormatInt(int64(v), 10)
	}
	msg := strings.TrimSpace(payload.Error.Message)
	if msg == "" {
		switch res.StatusCode {
		case http.StatusUnauthorized:
			msg = "unauthorized; check the Gateway key"
		case http.StatusForbidden:
			msg = "access denied by Magpie"
		case http.StatusNotFound:
			msg = "endpoint not available in this Magpie version"
		default:
			msg = http.StatusText(res.StatusCode)
		}
	}
	kind := ErrorHTTP
	switch res.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = ErrorAuth
	case http.StatusNotFound:
		kind = ErrorNotFound
	}
	var retryAfter time.Duration
	if s := res.Header.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			retryAfter = time.Duration(n) * time.Second
		}
	}
	return &APIError{Kind: kind, StatusCode: res.StatusCode, Code: code, Message: msg, RetryAfter: retryAfter}
}

// IsAuthError reports an authentication or permission failure.
func IsAuthError(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Kind == ErrorAuth
}

// IsNotFound reports an endpoint absent from the running Magpie version.
func IsNotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Kind == ErrorNotFound
}

// IsTransportError reports a network or timeout failure.
func IsTransportError(err error) bool {
	var e *APIError
	return errors.As(err, &e) && (e.Kind == ErrorNetwork || e.Kind == ErrorTimeout)
}

// Concurrency is GET /v1/magpie/concurrency.
type Concurrency struct {
	Object   string          `json:"object"`
	Lanes    map[string]Lane `json:"data"`
	Requests int             `json:"requests"`
	Waiting  int             `json:"waiting"`
	Tools    int             `json:"tools"`
	Active   []ActiveRequest `json:"active"`
	ReadAt   time.Time       `json:"-"`
}

// Lane is one provider key or account's concurrency.
type Lane struct {
	Busy    int `json:"busy"`
	Waiting int `json:"waiting"`
	Limit   int `json:"limit"`
}

// ActiveRequest is a row of the concurrency `active` array.
type ActiveRequest struct {
	ID       int64     `json:"id"`
	Agent    string    `json:"agent"`
	Model    string    `json:"model,omitempty"`
	Provider string    `json:"provider,omitempty"`
	Session  string    `json:"session,omitempty"`
	Started  time.Time `json:"started"`
	Millis   int64     `json:"ms"`
	Phase    string    `json:"phase,omitempty"`
	RouteID  int64     `json:"route,omitempty"`
}

func (c *Client) Concurrency(ctx context.Context) (Concurrency, error) {
	var out Concurrency
	err := c.get(ctx, "/v1/magpie/concurrency", nil, &out)
	if err == nil && out.Object != "" && out.Object != "concurrency" {
		err = &APIError{Kind: ErrorProtocol, Message: "unexpected concurrency object"}
	}
	out.ReadAt = time.Now()
	return out, err
}

// RouteResponse is GET /v1/magpie/route.
type RouteResponse struct {
	Session string        `json:"session"`
	Seq     int64         `json:"seq"`
	Route   *SessionRoute `json:"route"`
}

// SessionRoute is Magpie's public per-session route representation.
type SessionRoute struct {
	ID          int64        `json:"id"`
	Time        time.Time    `json:"time"`
	Kind        string       `json:"kind,omitempty"`
	Asked       string       `json:"asked"`
	AskedEffort string       `json:"asked_effort,omitempty"`
	AskedTier   string       `json:"asked_tier,omitempty"`
	Group       string       `json:"group,omitempty"`
	Rule        *RouteRule   `json:"rule,omitempty"`
	Nested      []NestedRule `json:"nested,omitempty"`
	Model       string       `json:"model,omitempty"`
	Effort      string       `json:"effort,omitempty"`
	Tier        string       `json:"tier,omitempty"`
	TierServed  string       `json:"tier_served,omitempty"`
	Served      string       `json:"served,omitempty"`
	Tries       []RouteTry   `json:"tries"`
	Done        bool         `json:"done"`
	Status      int          `json:"status,omitempty"`
	Error       string       `json:"error,omitempty"`
	Millis      int64        `json:"ms,omitempty"`
	TTFT        int64        `json:"ttft_ms,omitempty"`
	TTFTOld     int64        `json:"ttft,omitempty"`
}

// RouteTry is one attempt in a SessionRoute.
type RouteTry struct {
	Model      string `json:"model"`
	Effort     string `json:"effort,omitempty"`
	Tier       string `json:"tier,omitempty"`
	TierServed string `json:"tier_served,omitempty"`
	Done       bool   `json:"done"`
	Status     int    `json:"status,omitempty"`
	Fail       string `json:"fail,omitempty"`
	Millis     int64  `json:"ms,omitempty"`
	TTFT       int64  `json:"ttft,omitempty"`
	TTFTMS     int64  `json:"ttft_ms,omitempty"`
}

// RouteRule is the subset of a rule decision exposed by the public API.
type RouteRule struct {
	N    int      `json:"n"`
	Use  string   `json:"use,omitempty"`
	When []string `json:"when,omitempty"`
}

type NestedRule struct {
	Group string     `json:"group"`
	Name  string     `json:"name,omitempty"`
	Rule  *RouteRule `json:"rule,omitempty"`
}

// SessionRoute fetches the latest route for a session. after and wait enable
// Magpie's long poll; wait is capped by the server at 60 seconds.
func (c *Client) SessionRoute(ctx context.Context, session string, after int64, wait time.Duration) (RouteResponse, error) {
	q := url.Values{"session": []string{session}}
	if after > 0 {
		q.Set("after", strconv.FormatInt(after, 10))
	}
	if wait > 0 {
		q.Set("wait", strconv.FormatFloat(wait.Seconds(), 'f', 3, 64))
	}
	var out RouteResponse
	err := c.get(ctx, "/v1/magpie/route", q, &out)
	return out, err
}

// Model is one GET /v1/models entry.
type Model struct {
	ID              string   `json:"id"`
	Object          string   `json:"object,omitempty"`
	OwnedBy         string   `json:"owned_by,omitempty"`
	DisplayName     string   `json:"display_name,omitempty"`
	MagpieLabel     string   `json:"magpie_label,omitempty"`
	ContextWindow   int      `json:"context_window,omitempty"`
	MaxInputTokens  int      `json:"max_input_tokens,omitempty"`
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"`
	Reasoning       bool     `json:"reasoning,omitempty"`
	ReasoningLevels []string `json:"reasoning_levels,omitempty"`
	SupportedLevels []struct {
		Effort string `json:"effort"`
	} `json:"supported_reasoning_levels,omitempty"`
	Group  string `json:"group,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Free   bool   `json:"free,omitempty"`
	Images bool   `json:"images,omitempty"`
}

type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var out ModelsResponse
	if err := c.get(ctx, "/v1/models", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Quota is one GET /v1/magpie/quotas entry.
type Quota struct {
	Provider string      `json:"provider"`
	Name     string      `json:"name"`
	Kind     string      `json:"kind"`
	Plan     string      `json:"plan,omitempty"`
	User     string      `json:"user,omitempty"`
	Windows  []QuotaSpan `json:"windows"`
	Balance  string      `json:"balance,omitempty"`
	Error    string      `json:"error,omitempty"`
	ReadAt   *time.Time  `json:"readAt,omitempty"`
	Last     bool        `json:"last,omitempty"`
}

type QuotaSpan struct {
	Name      string     `json:"name"`
	Used      float64    `json:"used"`
	Remaining float64    `json:"remaining"`
	ResetsAt  *time.Time `json:"resetsAt,omitempty"`
	Display   string     `json:"display,omitempty"`
	Unit      string     `json:"unit,omitempty"`
}

type QuotasResponse struct {
	Object string  `json:"object"`
	Data   []Quota `json:"data"`
}

func (c *Client) Quotas(ctx context.Context) ([]Quota, error) {
	var out QuotasResponse
	if err := c.get(ctx, "/v1/magpie/quotas", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// UsageRow is the subset of the usage ledger that this app displays.
type UsageRow struct {
	Time       FlexibleTime `json:"time"`
	RouteID    int64        `json:"route,omitempty"`
	Agent      string       `json:"agent,omitempty"`
	Session    string       `json:"session,omitempty"`
	Model      string       `json:"model,omitempty"`
	Provider   string       `json:"provider,omitempty"`
	Account    string       `json:"account,omitempty"`
	Status     int          `json:"status,omitempty"`
	Millis     int64        `json:"ms,omitempty"`
	TTFT       int64        `json:"ttft,omitempty"`
	Input      int          `json:"in,omitempty"`
	Output     int          `json:"out,omitempty"`
	CacheRead  int          `json:"cacheRead,omitempty"`
	CacheWrite int          `json:"cacheWrite,omitempty"`
	Reasoning  int          `json:"reasoning,omitempty"`
	Retried    bool         `json:"retried,omitempty"`
	Failed     bool         `json:"failed,omitempty"`
}

// FlexibleTime accepts RFC 3339 strings and Unix milliseconds/seconds, which
// occur in different usage and history versions.
type FlexibleTime struct{ time.Time }

func (t *FlexibleTime) UnmarshalJSON(b []byte) error {
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 || string(b) == "null" {
		t.Time = time.Time{}
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			t.Time = time.Time{}
			return nil
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if x, err := time.Parse(layout, s); err == nil {
				t.Time = x
				return nil
			}
		}
		return fmt.Errorf("invalid time %q", s)
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	if n > 1_000_000_000_000 {
		t.Time = time.UnixMilli(n)
	} else {
		t.Time = time.Unix(n, 0)
	}
	return nil
}

type UsageRequests struct {
	Rows   []UsageRow `json:"rows"`
	Total  int        `json:"total"`
	Offset int        `json:"offset"`
	Period string     `json:"period,omitempty"`
	Bucket string     `json:"bucket,omitempty"`
}

func (c *Client) UsageRequests(ctx context.Context, offset, limit int) (UsageRequests, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := url.Values{
		"offset": []string{strconv.Itoa(max(offset, 0))},
		"limit":  []string{strconv.Itoa(limit)},
	}
	var out UsageRequests
	err := c.get(ctx, "/v1/magpie/usage/requests", q, &out)
	return out, err
}

// Usage is the summary endpoint, kept as a generic object because its shape
// is reported to evolve across Magpie versions.
type Usage map[string]any

func (c *Client) Usage(ctx context.Context, period string) (Usage, error) {
	var q url.Values
	if period != "" {
		q = url.Values{"period": []string{period}}
	}
	var out Usage
	err := c.get(ctx, "/v1/magpie/usage", q, &out)
	return out, err
}
