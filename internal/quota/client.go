package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// endpoint is undocumented. `claude auth status --json` carries no quota, so there
// is no supported alternative. When this breaks, acs degrades to an account
// switcher rather than showing numbers it cannot stand behind.
const endpoint = "https://api.anthropic.com/api/oauth/usage"

// betaHeader is required for OAuth-scoped requests to this endpoint.
const betaHeader = "oauth-2025-04-20"

// requestTimeout bounds one fetch. Quota is a nice-to-have; blocking a CLI on a
// hanging request is not.
const requestTimeout = 10 * time.Second

// maxBody caps how much of a response is read, so a wrong endpoint answering with
// something huge cannot exhaust memory.
const maxBody = 1 << 20

// errUnauthorized means the token was rejected outright.
var errUnauthorized = errors.New("credential rejected")

// throttleError carries the server's own retry timing.
type throttleError struct{ retryAfter time.Duration }

func (e throttleError) Error() string {
	return fmt.Sprintf("rate limited, retry in %s", e.retryAfter.Round(time.Second))
}

// client fetches usage. The zero value is usable.
type client struct {
	http    *http.Client
	baseURL string
	version string
}

func newClient(version string) *client {
	return &client{
		http:    &http.Client{Timeout: requestTimeout},
		baseURL: endpoint,
		version: version,
	}
}

// fetch calls the usage endpoint with a bearer token.
//
// Neither the request nor the response body is ever logged: the request carries
// the token, and the response is the user's own account data.
func (c *client) fetch(ctx context.Context, accessToken string) (apiResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return apiResponse{}, fmt.Errorf("build usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", betaHeader)
	req.Header.Set("Accept", "application/json")
	// An honest user agent. Impersonating claude-code is only worth considering if
	// this one turns out to be blocked.
	req.Header.Set("User-Agent", "acs/"+c.version)

	resp, err := c.http.Do(req)
	if err != nil {
		return apiResponse{}, fmt.Errorf("reach the usage endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return apiResponse{}, errUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests:
		return apiResponse{}, throttleError{retryAfter: retryAfterFrom(resp.Header)}
	case resp.StatusCode != http.StatusOK:
		return apiResponse{}, fmt.Errorf("usage endpoint returned HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return apiResponse{}, fmt.Errorf("read usage response: %w", err)
	}
	var out apiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		// The body is not echoed: it is account data.
		return apiResponse{}, fmt.Errorf("usage response is not the expected JSON: %w", err)
	}
	return out, nil
}

// retryAfterFrom prefers the server's own timing over any local guess: it knows
// when the window resets and acs does not.
func retryAfterFrom(h http.Header) time.Duration {
	value := h.Get("Retry-After")
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}
