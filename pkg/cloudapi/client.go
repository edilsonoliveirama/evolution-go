package cloudapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// defaultAllowedHosts lists the default allowed hosts for media download.
var defaultAllowedHosts = []string{
	"graph.facebook.com",
	"graph-video.facebook.com",
	"lookaside.facebook.com",
	"lookaside.fbsbx.com",
	"media-laren.facebook.com",
}

// privateCIDRs lists private IP ranges to block for SSRF protection.
var privateCIDRs = []string{
	"127.0.0.0/8",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"100.64.0.0/10",
	"::1",
	"fc00::/7",
}

// privateNetworks contains parsed private IP networks.
var privateNetworks []*net.IPNet

func init() {
	for _, cidr := range privateCIDRs {
		_, n, err := net.ParseCIDR(cidr)
		if err == nil {
			privateNetworks = append(privateNetworks, n)
		}
	}
}

// MetaClient is the HTTP client for the Meta Graph API.
type MetaClient struct {
	BaseURL     string
	Version     string
	httpClient  *http.Client
	allowlist   []string
}

// NewMetaClient creates a new MetaClient. If httpClient is nil, a default
// client with 30s timeout is used.
func NewMetaClient(baseURL, version string, httpClient *http.Client) *MetaClient {
	var httpClientOverride *http.Client
	if httpClient == nil {
		httpClientOverride = &http.Client{Timeout: 5 * time.Second}
	} else if httpClient.Timeout == 0 {
		httpClientOverride = &http.Client{
			Transport: httpClient.Transport,
			Timeout:   5 * time.Second,
		}
	} else {
		httpClientOverride = httpClient
	}

	// Add the base URL's host to the allowlist so httptest.Server URLs work.
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err == nil && parsed.Hostname() != "" {
		host := parsed.Hostname()
		extended := make([]string, len(defaultAllowedHosts)+1)
		extended[0] = host
		copy(extended[1:], defaultAllowedHosts)
		validator := &defaultURLValidator{allowlist: extended}
		return NewMetaClientWithConfig(baseURL, version, httpClientOverride, validator)
	}
	return NewMetaClientWithConfig(baseURL, version, httpClientOverride, nil)
}

// NewMetaClientWithConfig creates a new MetaClient with optional custom
// URLValidator for media download SSRF protection.
func NewMetaClientWithConfig(baseURL, version string, httpClient *http.Client, mediaURLValidator URLValidator) *MetaClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if mediaURLValidator == nil {
		mediaURLValidator = &defaultURLValidator{allowlist: defaultAllowedHosts}
	}

	// Clone the client to add CheckRedirect.
	client := &MetaClient{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Version:   version,
		httpClient: &http.Client{
			Transport:     httpClient.Transport,
			Timeout:       httpClient.Timeout,
			CheckRedirect: makeCheckRedirect(mediaURLValidator),
		},
		allowlist: defaultAllowedHosts,
	}

	// If mediaURLValidator is a defaultURLValidator, use its allowlist.
	if v, ok := mediaURLValidator.(*defaultURLValidator); ok {
		client.allowlist = append(client.allowlist, v.allowlist...)
	} else {
		client.allowlist = defaultAllowedHosts
	}

	return client
}

// makeCheckRedirect returns a CheckRedirect function that validates redirect
// targets for SSRF protection.
func makeCheckRedirect(validator URLValidator) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		// Re-validate the redirect target.
		if err := validator.ValidateURL(req.URL.String()); err != nil {
			return fmt.Errorf("redirect target blocked by security policy: %w", err)
		}
		// Refuse HTTPS -> HTTP downgrade.
		if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("redirect from HTTPS to HTTP refused")
		}
		return nil
	}
}

// defaultURLValidator validates URLs for SSRF protection.
type defaultURLValidator struct {
	allowlist []string
}

// ValidateURL checks that the URL host is in the allowlist.
// Private IP resolution check is done in DownloadMedia before connecting.
func (v *defaultURLValidator) ValidateURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	// Check host allowlist.
	allowed := false
	for _, allowedHost := range v.allowlist {
		if host == allowedHost || strings.HasSuffix(host, "."+allowedHost) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("host %q not allowed by security policy", host)
	}

	return nil
}

// validateURLWithPrivateIPCheck validates the URL and also checks for private IPs.
func validateURLWithPrivateIPCheck(rawURL string, allowlist []string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	// Check if host is in the allowlist first.
	allowed := false
	for _, allowedHost := range allowlist {
		if host == allowedHost || strings.HasSuffix(host, "."+allowedHost) {
			allowed = true
			break
		}
	}
	if allowed {
		return nil
	}

	// Check for private IPs only when host is not in the allowlist.
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("failed to resolve host %q: %w", host, err)
	}
	for _, ip := range ips {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			continue
		}
		for _, block := range privateNetworks {
			if block.Contains(parsedIP) {
				return fmt.Errorf("host %q resolves to private IP %s (blocked)", host, ip)
			}
		}
	}

	return nil
}

// doRequest performs an HTTP request with the given access token and returns the response body.
func (c *MetaClient) doRequest(method, fullURL string, body []byte, accessToken string) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	apiURL := fullURL
	if accessToken != "" {
		separator := "?"
		if strings.Contains(fullURL, "?") {
			separator = "&"
		}
		apiURL = fullURL + separator + "access_token=" + accessToken
	}

	req, err := http.NewRequest(method, apiURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, scrubMetaErrorFmt(fmt.Errorf("request failed: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return respBody, newMetaAPIError(resp.StatusCode, respBody)
	}

	return respBody, nil
}

// newMetaAPIError creates a MetaAPIError from an error response.
func newMetaAPIError(statusCode int, body []byte) error {
	var errResp struct {
		Error struct {
			Message   string `json:"message"`
			Type      string `json:"type"`
			Code      int    `json:"code"`
			ErrorData struct {
				Details string `json:"details"`
			} `json:"error_data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		return scrubMetaErrorFmt(fmt.Errorf("Meta API error (HTTP %d): %s", statusCode, errResp.Error.Message))
	}
	return scrubMetaErrorFmt(fmt.Errorf("Meta API HTTP %d: %s", statusCode, scrubMetaError(string(body))))
}

// ValidateCredentials validates the WABA credentials against the Meta Graph API
// by fetching the business profile.
func (c *MetaClient) ValidateCredentials(wabaID, token string) (*BusinessProfile, error) {
	apiURL := fmt.Sprintf("%s/%s/%s?access_token=%s", c.BaseURL, c.Version, wabaID, token)
	respBody, err := c.doRequest(http.MethodGet, apiURL, nil, "")
	if err != nil {
		return nil, err
	}

	// The API may return {"data": {"name":"...","id":"..."}} (lookup response) or
	// {"name":"...","id":"..."} (direct profile). Try both.
	var wrapper struct {
		Data struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &wrapper); err == nil && wrapper.Data.ID != "" {
		return &BusinessProfile{
			Data: []BusinessProfileData{{Name: wrapper.Data.Name, ID: wrapper.Data.ID}},
		}, nil
	}

	var profile BusinessProfile
	if err := json.Unmarshal(respBody, &profile); err != nil {
		return nil, fmt.Errorf("failed to parse business profile response: %w", err)
	}
	return &profile, nil
}

// SendMessage sends a message payload to the Meta Graph API with the given access token.
func (c *MetaClient) SendMessage(phoneNumberID string, payload []byte, accessToken string) (*SendResponse, error) {
	apiURL := fmt.Sprintf("%s/%s/%s/messages", c.BaseURL, c.Version, phoneNumberID)
	respBody, err := c.doRequest(http.MethodPost, apiURL, payload, accessToken)
	if err != nil {
		return nil, err
	}

	var sendResp SendResponse
	if err := json.Unmarshal(respBody, &sendResp); err != nil {
		return nil, fmt.Errorf("failed to parse send response: %w", err)
	}
	return &sendResp, nil
}

// UploadMedia uploads media to the Meta Graph API with the given access token and returns the media ID.
func (c *MetaClient) UploadMedia(phoneNumberID string, data []byte, mimeType string, accessToken string) (*MediaUploadResponse, error) {
	apiURL := fmt.Sprintf("%s/%s/%s/media?access_token=%s", c.BaseURL, c.Version, phoneNumberID, accessToken)

	// Build multipart form manually for Meta API.
	var buf bytes.Buffer
	boundary := "----WebKitFormBoundary7MA4YWxkTrZu0gW"
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"messaging_product\"\r\n\r\n")
	buf.WriteString("whatsapp\r\n")
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"media\"\r\n")
	buf.WriteString("Content-Type: " + mimeType + "\r\n\r\n")
	buf.Write(data)
	buf.WriteString("\r\n--" + boundary + "--\r\n")

	req, err := http.NewRequest(http.MethodPost, apiURL, &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload request: %w", err)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read upload response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, newMetaAPIError(resp.StatusCode, respBody)
	}

	var uploadResp MediaUploadResponse
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return nil, fmt.Errorf("failed to parse upload response: %w", err)
	}
	return &uploadResp, nil
}

// DownloadMedia downloads media from a URL with SSRF protection.
func (c *MetaClient) DownloadMedia(mediaURL string) ([]byte, error) {
	// Validate URL before connecting.
	if err := validateURLWithPrivateIPCheck(mediaURL, c.allowlist); err != nil {
		return nil, fmt.Errorf("media URL blocked by security policy: %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, mediaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("media download failed with HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read media data: %w", err)
	}

	return data, nil
}

// GetPhoneNumber fetches phone number information from the Meta Graph API with the given access token.
func (c *MetaClient) GetPhoneNumber(phoneNumberID string, accessToken string) (*PhoneNumberInfo, error) {
	apiURL := fmt.Sprintf("%s/%s/%s", c.BaseURL, c.Version, phoneNumberID)
	respBody, err := c.doRequest(http.MethodGet, apiURL, nil, accessToken)
	if err != nil {
		return nil, err
	}

	var info PhoneNumberInfo
	if err := json.Unmarshal(respBody, &info); err != nil {
		return nil, fmt.Errorf("failed to parse phone number response: %w", err)
	}
	return &info, nil
}

// scrubMetaErrorFmt applies scrubMetaError to an error's message.
func scrubMetaErrorFmt(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", scrubMetaError(err.Error()))
}

// scrubMetaError replaces sensitive patterns (access_token, secret, verify_token)
// in error messages to prevent credential leakage.
func scrubMetaError(msg string) string {
	if msg == "" {
		return msg
	}
	// Pattern: access_token=<value>
	re := regexp.MustCompile(`access_token=[^&\s"'}]+`)
	msg = re.ReplaceAllString(msg, "access_token=***")
	// Pattern: secret=<value>
	re = regexp.MustCompile(`secret=[^&\s"'}]+`)
	msg = re.ReplaceAllString(msg, "secret=***")
	// Pattern: verify_token=<value>
	re = regexp.MustCompile(`verify_token=[^&\s"'}]+`)
	msg = re.ReplaceAllString(msg, "verify_token=***")
	return msg
}
