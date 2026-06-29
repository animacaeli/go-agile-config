package agileconfig

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxErrorResponseBody = 16 * 1024

// apiConfig mirrors the AgileConfig API response item.
// The server returns JSON with PascalCase field names.
type apiConfig struct {
	Id           string `json:"Id"`
	AppId        string `json:"AppId"`
	Group        string `json:"Group"`
	Key          string `json:"Key"`
	Value        string `json:"Value"`
	Status       int    `json:"Status"`
	OnlineStatus int    `json:"OnlineStatus"`
	EditStatus   int    `json:"EditStatus"`
}

// transport handles HTTP requests to the AgileConfig server with Basic Auth.
type transport struct {
	serverURL         string
	appID             string
	secret            string
	client            *http.Client
	maxResponseBody   int64
	allowInsecureHTTP bool
}

func newTransport(serverURL, appID, secret string, opts options) *transport {
	return &transport{
		serverURL:         normalizeServerURL(serverURL),
		appID:             appID,
		secret:            secret,
		client:            newHTTPClient(opts),
		maxResponseBody:   opts.maxResponseBody,
		allowInsecureHTTP: opts.allowInsecureHTTP,
	}
}

func (t *transport) getServerURL() string { return t.serverURL }
func (t *transport) getAppID() string     { return t.appID }
func (t *transport) getSecret() string    { return t.secret }

func (t *transport) fetchConfigs(ctx context.Context, env string) ([]apiConfig, string, error) {
	u := fmt.Sprintf("%s/api/Config/app/%s", t.serverURL, url.PathEscape(t.appID))
	if env != "" {
		u += "?env=" + url.QueryEscape(env)
	}

	var configs []apiConfig
	resp, err := t.doJSON(ctx, http.MethodGet, u, nil, &configs)
	if err != nil {
		return nil, "", err
	}

	timelineID := resp.Header.Get("publish-time-line-id")
	return configs, timelineID, nil
}

func (t *transport) listServices(ctx context.Context, status ServiceQueryStatus) ([]ServiceInfo, error) {
	u := t.serverURL + "/api/RegisterCenter/services"
	switch status {
	case ServiceQueryStatusOnline:
		u += "/online"
	case ServiceQueryStatusOffline:
		u += "/offline"
	}

	var services []ServiceInfo
	if _, err := t.doJSON(ctx, http.MethodGet, u, nil, &services); err != nil {
		return nil, err
	}
	return services, nil
}

func (t *transport) registerService(ctx context.Context, service RegisterService) (RegisterResult, error) {
	u := t.serverURL + "/api/RegisterCenter"

	var result RegisterResult
	if _, err := t.doJSON(ctx, http.MethodPost, u, service, &result); err != nil {
		return RegisterResult{}, err
	}
	return result, nil
}

func (t *transport) unregisterService(ctx context.Context, uniqueID string) (RegisterResult, error) {
	u := fmt.Sprintf("%s/api/RegisterCenter/%s", t.serverURL, url.PathEscape(uniqueID))

	var result RegisterResult
	if _, err := t.doJSON(ctx, http.MethodDelete, u, nil, &result); err != nil {
		return RegisterResult{}, err
	}
	return result, nil
}

func (t *transport) heartbeat(ctx context.Context, uniqueID string) (HeartbeatResult, error) {
	u := t.serverURL + "/api/RegisterCenter/heartbeat"
	req := heartbeatRequest{UniqueID: uniqueID}

	var result HeartbeatResult
	if _, err := t.doJSON(ctx, http.MethodPost, u, req, &result); err != nil {
		return HeartbeatResult{}, err
	}
	return result, nil
}

func (t *transport) doJSON(ctx context.Context, method, requestURL string, payload any, out any) (*http.Response, error) {
	if err := validateServerURL(t.serverURL, t.allowInsecureHTTP); err != nil {
		return nil, err
	}

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encoding request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Basic "+basicAuth(t.appID, t.secret))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %s: %w", requestURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorResponseBody))
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	if out == nil {
		return resp, nil
	}

	body = io.LimitReader(resp.Body, t.maxResponseBody+1)
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if decoder.InputOffset() > t.maxResponseBody {
		return nil, fmt.Errorf("response exceeds %d bytes", t.maxResponseBody)
	}

	return resp, nil
}

// basicAuth returns a Base64-encoded "username:password" string for HTTP Basic Auth.
func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

func newHTTPClient(opts options) *http.Client {
	return &http.Client{
		Timeout: opts.httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if req.URL.Scheme == "https" {
				return nil
			}
			if req.URL.Scheme == "http" && opts.allowInsecureHTTP {
				return nil
			}
			return fmt.Errorf("refusing redirect to insecure URL %q", req.URL.String())
		},
	}
}

func normalizeServerURL(serverURL string) string {
	return strings.TrimRight(serverURL, "/")
}

func validateServerURL(serverURL string, allowInsecureHTTP bool) error {
	u, err := url.Parse(serverURL)
	if err != nil {
		return fmt.Errorf("invalid server URL %q: %w", serverURL, err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("invalid server URL %q: must start with http:// or https://", serverURL)
	}
	if u.Host == "" {
		return fmt.Errorf("invalid server URL %q: host is required", serverURL)
	}
	if u.Scheme == "http" && !allowInsecureHTTP {
		return fmt.Errorf("insecure server URL %q: use https:// or WithInsecureHTTP()", serverURL)
	}
	return nil
}
