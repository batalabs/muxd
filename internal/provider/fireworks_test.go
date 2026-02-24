package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/batalabs/muxd/internal/domain"
)

func TestFireworksProvider_Name(t *testing.T) {
	p := &FireworksProvider{}
	if got := p.Name(); got != "fireworks" {
		t.Errorf("Name() = %q, want %q", got, "fireworks")
	}
}

func TestFireworksProvider_FetchModels(t *testing.T) {
	t.Run("returns models on success", func(t *testing.T) {
		models := []domain.APIModelInfo{
			{ID: "accounts/fireworks/models/llama-v3p1-70b-instruct"},
			{ID: "accounts/fireworks/models/mixtral-8x7b-instruct"},
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Errorf("Authorization = %q, want %q", got, "Bearer test-key")
			}
			json.NewEncoder(w).Encode(map[string]any{"data": models})
		}))
		defer srv.Close()

		orig := fireworksAPIBaseURL
		setFireworksBaseURL(srv.URL)
		defer setFireworksBaseURL(orig)

		p := &FireworksProvider{}
		got, err := p.FetchModels("test-key")
		if err != nil {
			t.Fatalf("FetchModels() error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("FetchModels() returned %d models, want 2", len(got))
		}
		if got[0].ID != "accounts/fireworks/models/llama-v3p1-70b-instruct" {
			t.Errorf("got[0].ID = %q, want %q", got[0].ID, "accounts/fireworks/models/llama-v3p1-70b-instruct")
		}
	})

	t.Run("returns error on HTTP 401", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid key"}`))
		}))
		defer srv.Close()

		orig := fireworksAPIBaseURL
		setFireworksBaseURL(srv.URL)
		defer setFireworksBaseURL(orig)

		p := &FireworksProvider{}
		_, err := p.FetchModels("bad-key")
		if err == nil {
			t.Fatal("expected error on HTTP 401")
		}
	})

	t.Run("returns error on malformed JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`not json`))
		}))
		defer srv.Close()

		orig := fireworksAPIBaseURL
		setFireworksBaseURL(srv.URL)
		defer setFireworksBaseURL(orig)

		p := &FireworksProvider{}
		_, err := p.FetchModels("test-key")
		if err == nil {
			t.Fatal("expected error on malformed JSON")
		}
	})
}
