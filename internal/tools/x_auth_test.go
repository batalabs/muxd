package tools

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/batalabs/muxd/internal/config"
)

// ---------------------------------------------------------------------------
// parseXExpiry
// ---------------------------------------------------------------------------

func TestParseXExpiry(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantZero bool
		wantYear int
	}{
		{"valid RFC3339", "2026-02-20T10:00:00Z", false, 2026},
		{"unix timestamp", "1771581600", false, 2026},
		{"empty string", "", true, 0},
		{"whitespace only", "   ", true, 0},
		{"invalid string", "not-a-date", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseXExpiry(tt.input)
			if tt.wantZero {
				if !got.IsZero() {
					t.Errorf("parseXExpiry(%q) = %v, want zero", tt.input, got)
				}
			} else {
				if got.IsZero() {
					t.Fatalf("parseXExpiry(%q) returned zero", tt.input)
				}
				if got.Year() != tt.wantYear {
					t.Errorf("parseXExpiry(%q).Year() = %d, want %d", tt.input, got.Year(), tt.wantYear)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExchangeXOAuthCode
// ---------------------------------------------------------------------------

func TestExchangeXOAuthCode(t *testing.T) {
	origURL := xOAuthTokenURL
	origClient := xAuthHTTPClient
	t.Cleanup(func() {
		xOAuthTokenURL = origURL
		xAuthHTTPClient = origClient
	})

	t.Run("exchanges code successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Fatalf("method = %q, want POST", r.Method)
			}
			if !strings.Contains(r.Header.Get("Content-Type"), "x-www-form-urlencoded") {
				t.Fatalf("content type = %q", r.Header.Get("Content-Type"))
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token_type":"bearer","expires_in":7200,"access_token":"new-access","refresh_token":"new-refresh","scope":"tweet.read"}`)
		}))
		defer srv.Close()
		xOAuthTokenURL = srv.URL
		xAuthHTTPClient = srv.Client()

		tok, err := ExchangeXOAuthCode("client-id", "client-secret", "auth-code", "http://localhost/callback", "verifier")
		if err != nil {
			t.Fatalf("ExchangeXOAuthCode error: %v", err)
		}
		if tok.AccessToken != "new-access" {
			t.Errorf("access_token = %q", tok.AccessToken)
		}
		if tok.RefreshToken != "new-refresh" {
			t.Errorf("refresh_token = %q", tok.RefreshToken)
		}
	})

	t.Run("returns error on HTTP failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_grant"}`)
		}))
		defer srv.Close()
		xOAuthTokenURL = srv.URL
		xAuthHTTPClient = srv.Client()

		_, err := ExchangeXOAuthCode("client-id", "", "bad-code", "http://localhost/callback", "verifier")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "token exchange failed") {
			t.Errorf("error = %q", err.Error())
		}
	})

	t.Run("returns error on empty access_token", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"token_type":"bearer","access_token":"","refresh_token":"r"}`)
		}))
		defer srv.Close()
		xOAuthTokenURL = srv.URL
		xAuthHTTPClient = srv.Client()

		_, err := ExchangeXOAuthCode("cid", "", "code", "http://localhost/callback", "v")
		if err == nil || !strings.Contains(err.Error(), "empty access_token") {
			t.Fatalf("expected empty access_token error, got: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// RefreshXOAuthToken
// ---------------------------------------------------------------------------

func TestRefreshXOAuthToken(t *testing.T) {
	origURL := xOAuthTokenURL
	origClient := xAuthHTTPClient
	t.Cleanup(func() {
		xOAuthTokenURL = origURL
		xAuthHTTPClient = origClient
	})

	t.Run("refreshes token successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"token_type":"bearer","expires_in":3600,"access_token":"refreshed-access","refresh_token":"refreshed-refresh","scope":"tweet.read"}`)
		}))
		defer srv.Close()
		xOAuthTokenURL = srv.URL
		xAuthHTTPClient = srv.Client()

		tok, err := RefreshXOAuthToken("cid", "csecret", "old-refresh")
		if err != nil {
			t.Fatalf("RefreshXOAuthToken error: %v", err)
		}
		if tok.AccessToken != "refreshed-access" {
			t.Errorf("access_token = %q", tok.AccessToken)
		}
		if tok.RefreshToken != "refreshed-refresh" {
			t.Errorf("refresh_token = %q", tok.RefreshToken)
		}
	})

	t.Run("keeps old refresh token when new is empty", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"token_type":"bearer","expires_in":3600,"access_token":"new","refresh_token":"","scope":"tweet.read"}`)
		}))
		defer srv.Close()
		xOAuthTokenURL = srv.URL
		xAuthHTTPClient = srv.Client()

		tok, err := RefreshXOAuthToken("cid", "", "kept-refresh")
		if err != nil {
			t.Fatalf("RefreshXOAuthToken error: %v", err)
		}
		if tok.RefreshToken != "kept-refresh" {
			t.Errorf("expected kept-refresh, got %q", tok.RefreshToken)
		}
	})

	t.Run("returns error on HTTP failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"invalid_client"}`)
		}))
		defer srv.Close()
		xOAuthTokenURL = srv.URL
		xAuthHTTPClient = srv.Client()

		_, err := RefreshXOAuthToken("cid", "csecret", "bad-refresh")
		if err == nil || !strings.Contains(err.Error(), "token refresh failed") {
			t.Fatalf("expected token refresh failed error, got: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// StartXOAuthLocal
// ---------------------------------------------------------------------------

func TestStartXOAuthLocal(t *testing.T) {
	authURL, waitCode, closeFn, err := StartXOAuthLocal("client-id", []string{"tweet.read", "tweet.write"}, "")
	if err != nil {
		t.Fatalf("StartXOAuthLocal error: %v", err)
	}
	defer closeFn()

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	redirectURI := parsed.Query().Get("redirect_uri")
	state := parsed.Query().Get("state")
	if redirectURI == "" || state == "" {
		t.Fatalf("missing redirect_uri/state in auth URL: %s", authURL)
	}

	_, err = http.Get(redirectURI + "?code=abc123&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	got, err := waitCode(2 * time.Second)
	if err != nil {
		t.Fatalf("waitCode error: %v", err)
	}
	if !strings.HasPrefix(got, "abc123|") {
		t.Fatalf("unexpected callback payload: %q", got)
	}
}

func TestStartXOAuthLocal_portsInUse(t *testing.T) {
	// Occupy both callback ports to trigger the error path.
	ln1, err := net.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		t.Skipf("cannot bind port 8080 for test: %v", err)
	}
	defer ln1.Close()

	ln2, err := net.Listen("tcp", "127.0.0.1:8089")
	if err != nil {
		t.Skipf("cannot bind port 8089 for test: %v", err)
	}
	defer ln2.Close()

	_, _, _, err = StartXOAuthLocal("client-id", []string{"tweet.read"}, "")
	if err == nil {
		t.Fatal("expected error when both ports are occupied")
	}
	if !strings.Contains(err.Error(), "callback ports in use") {
		t.Errorf("expected 'callback ports in use' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "close stale muxd processes") {
		t.Errorf("expected hint about stale processes, got: %v", err)
	}
}

func TestResolveXPostTokenFromPrefs(t *testing.T) {
	t.Run("uses active access token", func(t *testing.T) {
		p := &config.Preferences{
			XAccessToken: "access-1",
			XTokenExpiry: time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339),
		}
		token, refreshed, err := ResolveXPostTokenFromPrefs(p)
		if err != nil {
			t.Fatalf("ResolveXPostTokenFromPrefs error: %v", err)
		}
		if token != "access-1" || refreshed {
			t.Fatalf("unexpected token/refreshed: %q %v", token, refreshed)
		}
	})

	t.Run("refreshes expired access token", func(t *testing.T) {
		origURL := xOAuthTokenURL
		origClient := xAuthHTTPClient
		t.Cleanup(func() {
			xOAuthTokenURL = origURL
			xAuthHTTPClient = origClient
		})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"token_type":"bearer","expires_in":3600,"access_token":"new-access","refresh_token":"new-refresh","scope":"tweet.read tweet.write"}`)
		}))
		defer srv.Close()
		xOAuthTokenURL = srv.URL
		xAuthHTTPClient = srv.Client()

		p := &config.Preferences{
			XClientID:     "client-id",
			XClientSecret: "client-secret",
			XAccessToken:  "old-access",
			XRefreshToken: "old-refresh",
			XTokenExpiry:  time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339),
		}
		token, refreshed, err := ResolveXPostTokenFromPrefs(p)
		if err != nil {
			t.Fatalf("ResolveXPostTokenFromPrefs error: %v", err)
		}
		if token != "new-access" || !refreshed {
			t.Fatalf("unexpected token/refreshed: %q %v", token, refreshed)
		}
		if p.XRefreshToken != "new-refresh" {
			t.Fatalf("expected refresh token update, got %q", p.XRefreshToken)
		}
	})
}
