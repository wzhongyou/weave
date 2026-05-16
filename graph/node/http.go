package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wzhongyou/graphflow/graph"
)

// HTTPConfig configures an HTTP node.
type HTTPConfig[S any] struct {
	Method  string        // GET, POST, …
	URL     string        // static URL; use URLFunc for dynamic
	URLFunc func(S) string // overrides URL if set
	Timeout time.Duration

	// BuildRequest lets callers customise headers and body from state.
	// If nil, a JSON-encoded state is used as the body for non-GET methods.
	BuildRequest func(ctx context.Context, state S, req *http.Request) error

	// ParseResponse merges the HTTP response into state.
	// If nil, the response body is discarded.
	ParseResponse func(ctx context.Context, state S, resp *http.Response) (S, error)
}

// HTTP builds a NodeFunc that makes an outbound HTTP call.
func HTTP[S any](cfg HTTPConfig[S]) graph.NodeFunc[S] {
	client := &http.Client{Timeout: cfg.Timeout}
	if cfg.Timeout == 0 {
		client.Timeout = 30 * time.Second
	}

	return func(ctx context.Context, state S) (S, error) {
		url := cfg.URL
		if cfg.URLFunc != nil {
			url = cfg.URLFunc(state)
		}

		var bodyReader io.Reader
		if cfg.Method != http.MethodGet && cfg.Method != http.MethodHead {
			b, err := json.Marshal(state)
			if err != nil {
				return state, fmt.Errorf("http node: marshal body: %w", err)
			}
			bodyReader = strings.NewReader(string(b))
		}

		req, err := http.NewRequestWithContext(ctx, cfg.Method, url, bodyReader)
		if err != nil {
			return state, fmt.Errorf("http node: build request: %w", err)
		}
		if bodyReader != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		if cfg.BuildRequest != nil {
			if err := cfg.BuildRequest(ctx, state, req); err != nil {
				return state, fmt.Errorf("http node: customise request: %w", err)
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			return state, fmt.Errorf("http node: do request: %w", err)
		}
		defer resp.Body.Close()

		if cfg.ParseResponse != nil {
			return cfg.ParseResponse(ctx, state, resp)
		}
		return state, nil
	}
}
