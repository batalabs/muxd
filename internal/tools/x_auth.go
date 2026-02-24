package tools

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/config"
)

const xOAuthAuthorizeURL = "https://twitter.com/i/oauth2/authorize"

var xOAuthTokenURL = "https://api.x.com/2/oauth2/token"
var xOAuthDefaultRedirectURL = "http://127.0.0.1:8080/callback"

var xAuthHTTPClient = &http.Client{Timeout: 20 * time.Second}

// XOAuthToken is the normalized token payload we store/use.
type XOAuthToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Scope        string
	TokenType    string
}

type xTokenResponse struct {
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	AccessToken  string `json:"access_token"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token"`
}

// StartXOAuthLocal starts a local callback server and returns an auth URL
// plus a blocking wait function to receive the authorization code.
func StartXOAuthLocal(clientID string, scopes []string, redirectOverride string) (authURL string, waitCode func(timeout time.Duration) (string, error), closeFn func(), err error) {
	if strings.TrimSpace(clientID) == "" {
		return "", nil, nil, fmt.Errorf("x.client_id is required")
	}
	codeVerifier, codeChallenge, err := generatePKCE()
	if err != nil {
		return "", nil, nil, fmt.Errorf("pkce generation: %w", err)
	}
	state, err := randomURLSafe(24)
	if err != nil {
		return "", nil, nil, fmt.Errorf("state generation: %w", err)
	}

	redirectURL := strings.TrimSpace(redirectOverride)
	if redirectURL == "" {
		redirectURL = xOAuthDefaultRedirectURL
	}
	redirectParsed, err := url.Parse(redirectURL)
	if err != nil {
		return "", nil, nil, fmt.Errorf("invalid redirect url: %w", err)
	}
	if redirectParsed.Host == "" {
		return "", nil, nil, fmt.Errorf("invalid redirect url host")
	}
	// Try primary port, then registered fallback ports.
	// NOTE: all callback URLs must be registered in the X Developer Portal.
	host, _, _ := net.SplitHostPort(redirectParsed.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	ports := []string{redirectParsed.Host, host + ":8089"}
	var ln net.Listener
	var triedAddrs []string
	for _, addr := range ports {
		ln, err = net.Listen("tcp", addr)
		if err == nil {
			redirectParsed.Host = addr
			redirectURL = redirectParsed.String()
			break
		}
		triedAddrs = append(triedAddrs, addr)
	}
	if ln == nil {
		return "", nil, nil, fmt.Errorf(
			"callback ports in use (%s); close stale muxd processes or other listeners on these ports and retry",
			strings.Join(triedAddrs, ", "),
		)
	}

	callbackCh := make(chan struct {
		code  string
		state string
		err   string
	}, 1)

	mux := http.NewServeMux()
	callbackPath := redirectParsed.Path
	if callbackPath == "" {
		callbackPath = "/callback"
	}
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		callbackCh <- struct {
			code  string
			state string
			err   string
		}{
			code:  strings.TrimSpace(q.Get("code")),
			state: strings.TrimSpace(q.Get("state")),
			err:   strings.TrimSpace(q.Get("error")),
		}
		if _, err := io.WriteString(w, "X auth complete. You can close this tab and return to muxd."); err != nil {
			// HTTP response write; client may have disconnected
		}
	})
	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "x: oauth callback server: %v\n", err)
		}
	}()

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", clientID)
	values.Set("redirect_uri", redirectURL)
	values.Set("scope", strings.Join(scopes, " "))
	values.Set("state", state)
	values.Set("code_challenge", codeChallenge)
	values.Set("code_challenge_method", "S256")
	authURL = xOAuthAuthorizeURL + "?" + values.Encode()

	waitCode = func(timeout time.Duration) (string, error) {
		if timeout <= 0 {
			timeout = 3 * time.Minute
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case cb := <-callbackCh:
			if cb.err != "" {
				return "", fmt.Errorf("X auth error: %s", cb.err)
			}
			if cb.state != state {
				return "", fmt.Errorf("X callback state mismatch")
			}
			if cb.code == "" {
				return "", fmt.Errorf("X callback missing authorization code")
			}
			return cb.code + "|" + redirectURL + "|" + codeVerifier, nil
		case <-timer.C:
			return "", fmt.Errorf("X auth timed out waiting for callback")
		}
	}

	closeFn = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "x: shutdown oauth server: %v\n", err)
		}
		if err := ln.Close(); err != nil && !strings.Contains(err.Error(), "use of closed") {
			fmt.Fprintf(os.Stderr, "x: close oauth listener: %v\n", err)
		}
	}
	return authURL, waitCode, closeFn, nil
}

// ExchangeXOAuthCode exchanges auth code for access/refresh tokens.
func ExchangeXOAuthCode(clientID, clientSecret, code, redirectURL, codeVerifier string) (XOAuthToken, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURL)
	form.Set("code_verifier", codeVerifier)
	form.Set("client_id", clientID)

	req, err := http.NewRequest(http.MethodPost, xOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return XOAuthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if strings.TrimSpace(clientSecret) != "" {
		basic := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
		req.Header.Set("Authorization", "Basic "+basic)
	}

	resp, err := xAuthHTTPClient.Do(req)
	if err != nil {
		return XOAuthToken{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return XOAuthToken{}, fmt.Errorf("X token exchange failed (HTTP %d): %s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 500))
	}

	var out xTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return XOAuthToken{}, fmt.Errorf("parse token response: %w", err)
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return XOAuthToken{}, fmt.Errorf("X token exchange returned empty access_token")
	}
	return xTokenFromResponse(out), nil
}

// RefreshXOAuthToken refreshes an OAuth access token using refresh token.
func RefreshXOAuthToken(clientID, clientSecret, refreshToken string) (XOAuthToken, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)

	req, err := http.NewRequest(http.MethodPost, xOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return XOAuthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if strings.TrimSpace(clientSecret) != "" {
		basic := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
		req.Header.Set("Authorization", "Basic "+basic)
	}

	resp, err := xAuthHTTPClient.Do(req)
	if err != nil {
		return XOAuthToken{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return XOAuthToken{}, fmt.Errorf("X token refresh failed (HTTP %d): %s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 500))
	}

	var out xTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return XOAuthToken{}, fmt.Errorf("parse refresh response: %w", err)
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return XOAuthToken{}, fmt.Errorf("X refresh returned empty access_token")
	}
	if strings.TrimSpace(out.RefreshToken) == "" {
		out.RefreshToken = refreshToken
	}
	return xTokenFromResponse(out), nil
}

// ResolveXPostTokenFromPrefs returns best token for posting, refreshing if needed.
func ResolveXPostTokenFromPrefs(prefs *config.Preferences) (string, bool, error) {
	if prefs == nil {
		return "", false, fmt.Errorf("preferences are required")
	}
	now := time.Now().UTC()
	access := strings.TrimSpace(prefs.XAccessToken)
	expiry := parseXExpiry(prefs.XTokenExpiry)
	if access != "" && (expiry.IsZero() || now.Before(expiry.Add(-60*time.Second))) {
		return access, false, nil
	}
	if strings.TrimSpace(prefs.XRefreshToken) != "" && strings.TrimSpace(prefs.XClientID) != "" {
		tok, err := RefreshXOAuthToken(prefs.XClientID, prefs.XClientSecret, prefs.XRefreshToken)
		if err == nil {
			prefs.XAccessToken = tok.AccessToken
			prefs.XRefreshToken = tok.RefreshToken
			prefs.XTokenExpiry = tok.ExpiresAt.UTC().Format(time.RFC3339)
			return tok.AccessToken, true, nil
		}
	}
	if v := strings.TrimSpace(getEnvFunc("X_BEARER_TOKEN")); v != "" {
		return v, false, nil
	}
	return "", false, fmt.Errorf("X authentication not configured")
}

// OpenBrowser opens the URL using platform default browser.
func OpenBrowser(url string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("url is required")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func xTokenFromResponse(resp xTokenResponse) XOAuthToken {
	expiresAt := time.Now().UTC().Add(time.Duration(resp.ExpiresIn) * time.Second)
	return XOAuthToken{
		AccessToken:  strings.TrimSpace(resp.AccessToken),
		RefreshToken: strings.TrimSpace(resp.RefreshToken),
		ExpiresAt:    expiresAt,
		Scope:        strings.TrimSpace(resp.Scope),
		TokenType:    strings.TrimSpace(resp.TokenType),
	}
}

func parseXExpiry(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	if unix, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(unix, 0).UTC()
	}
	return time.Time{}
}

func generatePKCE() (verifier string, challenge string, err error) {
	verifier, err = randomURLSafe(64)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func randomURLSafe(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
