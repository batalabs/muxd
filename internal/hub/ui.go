package hub

import (
	"fmt"
	"net/http"
)

// uiFileServer is a placeholder for the hub management UI.
// To embed UI assets, place them in internal/hub/ui/ and replace
// this stub with the embed version.
type uiFileServer struct{}

func newUIFileServer() (*uiFileServer, error) {
	return nil, fmt.Errorf("hub UI not embedded")
}

func (s *uiFileServer) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "hub UI not available", http.StatusNotFound)
}
