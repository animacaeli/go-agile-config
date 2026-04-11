package agileconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

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
	serverURL string
	appID     string
	secret    string
	client    *http.Client
}

func newTransport(serverURL, appID, secret string, timeout time.Duration) *transport {
	return &transport{
		serverURL: serverURL,
		appID:     appID,
		secret:    secret,
		client:    &http.Client{Timeout: timeout},
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Basic "+basicAuth(t.appID, t.secret))

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching configs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var configs []apiConfig
	if err := json.NewDecoder(resp.Body).Decode(&configs); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}

	timelineID := resp.Header.Get("publish-time-line-id")
	return configs, timelineID, nil
}

// basicAuth returns a Base64-encoded "username:password" string for HTTP Basic Auth.
func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}
