package magpie

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func TestClientConcurrencyAndAuth(t *testing.T) {
	const key = "test-secret"
	var gotAuth, gotPath string
	s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"concurrency","data":{"p#1":{"busy":1,"waiting":2,"limit":3}},"requests":4,"waiting":2,"tools":0,"active":[{"id":9,"agent":"codex","model":"p/m","provider":"p","session":"s","started":"2026-10-18T10:00:00Z","ms":120,"phase":"upstream","route":7001}]}`))
	})
	c, err := NewClient(Options{BaseURL: s.URL, APIKey: key, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Concurrency(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer "+key || gotPath != "/v1/magpie/concurrency" {
		t.Fatalf("request auth/path = %q %q", gotAuth, gotPath)
	}
	if got.Requests != 4 || len(got.Active) != 1 || got.Active[0].RouteID != 7001 {
		t.Fatalf("unexpected response %+v", got)
	}
	if got.Lanes["p#1"].Waiting != 2 {
		t.Fatalf("lanes = %+v", got.Lanes)
	}
}

func TestClientSessionRouteQuery(t *testing.T) {
	var query string
	s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"session":"abc","seq":12,"route":{"id":7001,"time":"2026-10-18T10:00:00Z","asked":"p/m","model":"p/m","effort":"high","tries":[{"model":"p/m","done":true,"status":200,"ms":12}],"done":true,"status":200,"ms":12,"ttft_ms":5}}`))
	})
	c, _ := NewClient(Options{BaseURL: s.URL})
	got, err := c.SessionRoute(context.Background(), "abc", 4, 1500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "session=abc") || !strings.Contains(query, "after=4") || !strings.Contains(query, "wait=1.500") {
		t.Fatalf("query = %q", query)
	}
	if got.Route == nil || got.Route.ID != 7001 || got.Seq != 12 {
		t.Fatalf("response = %+v", got)
	}
}

func TestClientHTTPErrors(t *testing.T) {
	tests := []struct {
		status  int
		body    string
		kind    ErrorKind
		message string
	}{
		{http.StatusUnauthorized, `{"error":{"message":"bad key"}}`, ErrorAuth, "bad key"},
		{http.StatusForbidden, `{"error":{"message":"no accounts"}}`, ErrorAuth, "no accounts"},
		{http.StatusNotFound, `{}`, ErrorNotFound, "endpoint not available"},
		{http.StatusInternalServerError, `{"error":{"message":"boom"}}`, ErrorHTTP, "boom"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			c, _ := NewClient(Options{BaseURL: s.URL, APIKey: "must-not-leak"})
			_, err := c.Concurrency(context.Background())
			var api *APIError
			if !errors.As(err, &api) || api.Kind != tt.kind || api.StatusCode != tt.status {
				t.Fatalf("error = %#v", err)
			}
			if !strings.Contains(api.Message, tt.message) {
				t.Fatalf("message = %q", api.Message)
			}
			if strings.Contains(err.Error(), "must-not-leak") {
				t.Fatalf("error leaked key: %v", err)
			}
		})
	}
}

func TestClientMalformedAndTimeout(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"active":`))
		})
		c, _ := NewClient(Options{BaseURL: s.URL})
		_, err := c.Concurrency(context.Background())
		var api *APIError
		if !errors.As(err, &api) || api.Kind != ErrorProtocol {
			t.Fatalf("error = %#v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		block := make(chan struct{})
		s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-block:
			case <-r.Context().Done():
			}
		})
		t.Cleanup(func() { close(block) })
		c, _ := NewClient(Options{BaseURL: s.URL, Timeout: 40 * time.Millisecond})
		_, err := c.Concurrency(context.Background())
		if !IsTransportError(err) {
			t.Fatalf("error = %#v", err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		})
		c, _ := NewClient(Options{BaseURL: s.URL})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := c.Concurrency(ctx)
		if !errors.Is(err, context.Canceled) && !IsTransportError(err) {
			t.Fatalf("error = %#v", err)
		}
	})
}

func TestClientModelsAndUsage(t *testing.T) {
	s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"p/m","display_name":"M","context_window":1234,"supported_reasoning_levels":[{"effort":"low"}]}]}`))
		case "/v1/magpie/usage/requests":
			if r.URL.Query().Get("limit") != "20" {
				t.Errorf("limit = %q", r.URL.Query().Get("limit"))
			}
			_, _ = w.Write([]byte(`{"rows":[{"time":"2026-10-18T10:00:00Z","route":7001,"model":"p/m","in":3,"out":2}],"total":1,"offset":0}`))
		default:
			http.NotFound(w, r)
		}
	})
	c, _ := NewClient(Options{BaseURL: s.URL})
	models, err := c.Models(context.Background())
	if err != nil || len(models) != 1 || models[0].ContextWindow != 1234 {
		t.Fatalf("models = %+v, %v", models, err)
	}
	usage, err := c.UsageRequests(context.Background(), 0, 20)
	if err != nil || usage.Total != 1 || len(usage.Rows) != 1 || usage.Rows[0].Time.IsZero() {
		t.Fatalf("usage = %+v, %v", usage, err)
	}
}

func TestNewClientRejectsUnsafeURL(t *testing.T) {
	for _, u := range []string{"", "127.0.0.1:3425", "ftp://example.com", "http://user:pass@example.com"} {
		if _, err := NewClient(Options{BaseURL: u}); err == nil {
			t.Errorf("NewClient(%q) succeeded", u)
		}
	}
}
