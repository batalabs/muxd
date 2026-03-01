package hub

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func (h *Hub) handleProxy(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeID")
	path := r.PathValue("path")

	node := h.getNode(nodeID)
	if node == nil {
		writeHubJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}
	if node.Status != StatusOnline {
		writeHubJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "node is offline"})
		return
	}

	target, err := url.Parse(fmt.Sprintf("http://%s:%d", node.Host, node.Port))
	if err != nil {
		writeHubJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid node address"})
		return
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = "/" + path
			req.URL.RawQuery = r.URL.RawQuery
			req.Host = target.Host
			// Replace hub token with node's daemon token
			req.Header.Set("Authorization", "Bearer "+node.Token)
		},
	}
	proxy.ServeHTTP(w, r)
}
