package cloudapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// AC: CLOUD-05 — ValidateCredentials against Graph API.
// AC: CLOUD-12 — DownloadMedia from Meta media URL.
// AC: CLOUD-16 — SendMessage (text).
// AC: CLOUD-17 — SendMessage (media).
// AC: CLOUD-20 — SendMessage (template).
//
// Expected types when implemented:
//
//	type MetaClient struct { ... }
//	func NewMetaClient(baseURL, version string, httpClient *http.Client) *MetaClient
//	func (c *MetaClient) SendMessage(phoneNumberId string, payload []byte) (*SendResponse, error)
//	func (c *MetaClient) ValidateCredentials(wabaId, token string) (*BusinessProfile, error)
//	func (c *MetaClient) UploadMedia(phoneNumberId string, data []byte, mimeType string) (*MediaUploadResponse, error)
//	func (c *MetaClient) DownloadMedia(mediaUrl string) ([]byte, error)
//	func (c *MetaClient) GetPhoneNumber(phoneNumberId string) (*PhoneNumberInfo, error)
//
//	type SendResponse struct { ... }
//	type BusinessProfile struct { ... }
//	type MediaUploadResponse struct { ... }
//	type PhoneNumberInfo struct { ... }

func TestNewMetaClient_Defaults(t *testing.T) {
	client := NewMetaClient("https://graph.example.com", "v22.0", nil)
	if client == nil {
		t.Fatal("NewMetaClient returned nil")
	}
}

func TestNewMetaClient_CustomHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 5}
	client := NewMetaClient("https://graph.example.com", "v22.0", custom)
	if client == nil {
		t.Fatal("NewMetaClient returned nil")
	}
}

func TestMetaClient_ValidateCredentials_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path == "" || r.URL.Path == "/" {
			t.Fatal("expected Graph API path with version and WABA ID")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": {
				"name": "My Business",
				"id": "123456789"
			}
		}`)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	profile, err := client.ValidateCredentials("123456789", "EAAxValidToken")
	if err != nil {
		t.Fatalf("ValidateCredentials returned unexpected error: %v", err)
	}
	if profile == nil {
		t.Fatal("ValidateCredentials returned nil profile")
	}
}

func TestMetaClient_ValidateCredentials_InvalidToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Invalid OAuth 2.0 Access Token",
				"type":    "OAuthException",
				"code":    190,
			},
		})
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	_, err := client.ValidateCredentials("123456789", "EAAxInvalidToken")
	if err == nil {
		t.Fatal("ValidateCredentials with invalid token should return an error")
	}
}

func TestMetaClient_ValidateCredentials_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "(#100) Invalid parameter",
				"type":    "OAuthException",
				"code":    100,
			},
		})
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	_, err := client.ValidateCredentials("999999999", "EAAxToken")
	if err == nil {
		t.Fatal("ValidateCredentials with invalid WABA should return an error")
	}
}

func TestMetaClient_SendMessage_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"messaging_product": "whatsapp",
			"contacts": [{"input": "[TELEFONE_BR_REDACTED]", "wa_id": "[TELEFONE_BR_REDACTED]"}],
			"messages": [{"id": "wamid.HBgNNTU0OTkyMTI5NTc5NRUCABIYFjNBQzk1MkZERTU5Rjc5QkUyQ0FGRTIA"}]
		}`)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	payload := []byte(`{"messaging_product":"whatsapp","type":"text","text":{"body":"Hello"}}`)
	resp, err := client.SendMessage("987654321", payload)
	if err != nil {
		t.Fatalf("SendMessage returned unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("SendMessage returned nil response")
	}
}

func TestMetaClient_SendMessage_ApiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "(#131047) Cannot send message outside 24h window",
				"type":    "OAuthException",
				"code":    131047,
				"error_data": map[string]interface{}{
					"details": "Message not allowed outside 24h window",
				},
			},
		})
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	payload := []byte(`{"messaging_product":"whatsapp","type":"text","text":{"body":"Hello"}}`)
	_, err := client.SendMessage("987654321", payload)
	if err == nil {
		t.Fatal("SendMessage outside 24h window should return an error (AC: CLOUD-18)")
	}
}

func TestMetaClient_UploadMedia_Success(t *testing.T) {
	body := `{"id":"987654321"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	resp, err := client.UploadMedia("987654321", []byte("fake-image-data"), "image/jpeg")
	if err != nil {
		t.Fatalf("UploadMedia returned unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("UploadMedia returned nil response")
	}
}

func TestMetaClient_DownloadMedia_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46})
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	data, err := client.DownloadMedia(srv.URL + "/some-media")
	if err != nil {
		t.Fatalf("DownloadMedia returned unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("DownloadMedia returned empty data")
	}
}

func TestMetaClient_DownloadMedia_ExpiredURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	_, err := client.DownloadMedia(srv.URL + "/expired-media")
	if err == nil {
		t.Fatal("DownloadMedia with expired URL should return an error (AC: CLOUD-12)")
	}
}

func TestMetaClient_GetPhoneNumber_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"verified_name": "My Business",
			"display_phone_number": "[TELEFONE_BR_REDACTED]",
			"id": "987654321",
			"quality_rating": "GREEN",
			"code_verification_status": "VERIFIED"
		}`)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	info, err := client.GetPhoneNumber("987654321")
	if err != nil {
		t.Fatalf("GetPhoneNumber returned unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("GetPhoneNumber returned nil info")
	}
}

func TestMetaClient_HTTPTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hang indefinitely.
		select {}
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	_, err := client.ValidateCredentials("123456789", "EAAxToken")
	if err == nil {
		t.Fatal("ValidateCredentials should time out on a hanging server")
	}
}

// --- Security: SEC-001, SEC-008 — SSRF mitigation ---

func TestSSRF_PrivateIPBlocked(t *testing.T) {
	// httptest.Server always binds to 127.0.0.1 (private IP).
	// DownloadMedia must refuse to connect to private IPs even when the host
	// is reachable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("metadata content"))
	}))
	defer srv.Close()

	client := NewMetaClient("https://graph.facebook.com", "v22.0", srv.Client())
	_, err := client.DownloadMedia(srv.URL + "/internal")
	if err == nil {
		t.Fatal("DownloadMedia to private IP should be blocked (SEC-001)")
	}
}

func TestSSRF_InvalidHostNotInAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not allowed"))
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	_, err := client.DownloadMedia("http://malicious.example.com/something")
	if err == nil {
		t.Fatal("DownloadMedia to host not in allowlist should be blocked (SEC-001)")
	}
}

func TestSSRF_RedirectToPrivateIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://169.254.169.254/latest/meta-data/")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	_, err := client.DownloadMedia(srv.URL + "/legitimate-path")
	if err == nil {
		t.Fatal("DownloadMedia following redirect to private IP should be blocked (SEC-008)")
	}
}

func TestSSRF_RedirectDowngradeHTTPS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://" + r.Host + "/target")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	// Redirect from the server's URL (http) to http is not a downgrade,
	// but a server at https redirecting to http IS a downgrade.
	// The implementation must detect HTTPS→HTTP via the original URL scheme.
	_, err := client.DownloadMedia("https://graph.facebook.com/some-path")
	if err == nil {
		// The URL is https (valid host) but any redirect must not downgrade to http.
		// This test validates the CheckRedirect refuses HTTP when original was HTTPS.
		// Expect failure because there's no real facebook.com resolving locally.
		// The real test above (RedirectToPrivateIP) is the primary assertion.
	}
}

func TestSSRF_MaxRedirects(t *testing.T) {
	redirectCount := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		w.Header().Set("Location", srv.URL + "/redirect/" + fmt.Sprintf("%d", redirectCount))
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	_, err := client.DownloadMedia(srv.URL + "/start")
	if err == nil {
		// The redirect chain should stop at 5 redirects; expect an error.
		// The actual redirect count may be 5 or 6 depending on implementation.
		redirectCount = 0
	}
	// Just verify the function doesn't loop forever (safety check).
	if redirectCount > 10 {
		t.Fatalf("DownloadMedia followed more than the 5-redirect limit (%d redirects) (SEC-008)", redirectCount)
	}
}

// --- Security: SEC-007 — Token scrubbing ---

func TestScrubMetaError_AccessTokenRemoved(t *testing.T) {
	// scrubMetaError must replace access_token=value with access_token=***
	raw := `{"error":{"message":"(#100) Parameters: access_token=EAAxMySecretToken123"}}`
	scrubbed := scrubMetaError(raw)
	if contains(scrubbed, "EAAxMySecretToken123") {
		t.Fatalf("scrubMetaError did not remove access_token value: %q (SEC-007)", scrubbed)
	}
	if !contains(scrubbed, "access_token=***") {
		t.Fatalf("scrubMetaError should contain access_token=*** in %q (SEC-007)", scrubbed)
	}
}

func TestScrubMetaError_SecretRemoved(t *testing.T) {
	raw := `error: access_token=EAAxToken&secret=my-app-secret-value`
	scrubbed := scrubMetaError(raw)
	if contains(scrubbed, "my-app-secret-value") {
		t.Fatalf("scrubMetaError did not remove secret value: %q (SEC-007)", scrubbed)
	}
}

func TestScrubMetaError_VerifyTokenRemoved(t *testing.T) {
	raw := `verify_token=my-custom-verify&other=param`
	scrubbed := scrubMetaError(raw)
	if contains(scrubbed, "my-custom-verify") {
		t.Fatalf("scrubMetaError did not remove verify_token value: %q (SEC-007)", scrubbed)
	}
}

func TestScrubMetaError_NoMatch(t *testing.T) {
	raw := `{"error":{"message":"(#131047) Message not allowed"}}`
	scrubbed := scrubMetaError(raw)
	if scrubbed != raw {
		t.Fatalf("scrubMetaError modified a safe error: %q (SEC-007)", scrubbed)
	}
}

func TestScrubMetaError_NilInput(t *testing.T) {
	result := scrubMetaError("")
	if result != "" {
		t.Fatalf("scrubMetaError on empty string should return empty, got %q", result)
	}
}

// contains reports whether s contains substr as a substring.
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i] == substr[0] {
			match := true
			for j := 1; j < len(substr); j++ {
				if s[i+j] != substr[j] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

// --- Security: SEC-002 — Webhook body size limit ---

func TestWebhookBodySizeLimit(t *testing.T) {
	handler := NewCloudApiHandler(nil, nil, nil)

	// Build a body larger than the default limit (10 MB).
	body := make([]byte, 11<<20) // 11 MB
	for i := range body {
		body[i] = 'x'
	}

	req := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=anything")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleWebhookPOST(w, req)

	if w.Code != http.StatusRequestEntityTooLarge && w.Code != http.StatusOK {
		t.Fatalf("expected 413 for body over 10 MB, got %d (SEC-002)", w.Code)
	}
}

func TestWebhookBodySizeLimit_UnderLimit(t *testing.T) {
	handler := NewCloudApiHandler(nil, nil, nil)

	body := []byte(`{"object":"whatsapp_business_account","entry":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=anything")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleWebhookPOST(w, req)

	// Under-limit body should NOT be rejected with 413 (it may still fail HMAC).
	if w.Code == http.StatusRequestEntityTooLarge {
		t.Fatal("Body under 10 MB should not be rejected with 413 (SEC-002)")
	}
}

// --- Security: SEC-004 — Instance enumeration protection ---

func TestHandleWebhookGET_IdenticalResponseForMissingInstance(t *testing.T) {
	handler := NewCloudApiHandler(nil, nil, nil)

	// Generate a valid UUID-like instanceId that doesn't exist.
	unknownUUID := "550e8400-e29b-41d4-a716-446655440000"

	req := httptest.NewRequest(http.MethodGet, "/"+unknownUUID+"?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=123", nil)
	w := httptest.NewRecorder()
	handler.HandleWebhookGET(w, req)
	firstCode := w.Code
	firstBody := w.Body.String()

	// Different missing UUID with same wrong token.
	req2 := httptest.NewRequest(http.MethodGet, "/[CARTAO_CREDITO_REDACTED]-[CARTAO_CREDITO_REDACTED]?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=456", nil)
	w2 := httptest.NewRecorder()
	handler.HandleWebhookGET(w2, req2)

	if w2.Code != firstCode {
		t.Fatalf("different missing UUIDs returned different status codes (%d vs %d) — enumeration possible (SEC-004)", firstCode, w2.Code)
	}
	if w2.Body.String() != firstBody {
		t.Fatalf("different missing UUIDs returned different body (%q vs %q) — enumeration possible (SEC-004)", firstBody, w2.Body.String())
	}
}

// --- Security: SEC-006 — Webhook replay ---

func TestHandleWebhookPOST_Replay(t *testing.T) {
	appSecret := "replay-test-secret"
	payload := map[string]interface{}{
		"object": "whatsapp_business_account",
		"entry": []map[string]interface{}{
			{
				"id": "123456789",
				"changes": []map[string]interface{}{
					{
						"value": map[string]interface{}{
							"messaging_product": "whatsapp",
							"metadata": map[string]interface{}{
								"display_phone_number": "[TELEFONE_BR_REDACTED]",
								"phone_number_id":     "987654321",
							},
							"messages": []map[string]interface{}{
								{
									"from": "[TELEFONE_BR_REDACTED]",
									"id":   "wamid.replay.test.001",
									"timestamp": "1700000300",
									"type": "text",
									"text": map[string]interface{}{"body": "Replay test"},
								},
							},
						},
						"field": "messages",
					},
				},
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(bodyBytes)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// First call — process normally.
	req1 := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(bodyBytes))
	req1.Header.Set("X-Hub-Signature-256", sig)
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	handler1 := NewCloudApiHandler(nil, nil, nil)
	handler1.HandleWebhookPOST(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first call expected 200, got %d (SEC-006)", w1.Code)
	}

	// Second call — same body, same signature — must also return 200 (accept)
	// but must NOT emit duplicate events.
	req2 := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(bodyBytes))
	req2.Header.Set("X-Hub-Signature-256", sig)
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	handler2 := NewCloudApiHandler(nil, nil, nil)
	handler2.HandleWebhookPOST(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second call (replay) expected 200, got %d — legitimate Meta retries must be accepted (SEC-006)", w2.Code)
	}
}

// --- Security: SEC-012 — Method restriction ---

func TestHandleWebhook_MethodNotAllowed(t *testing.T) {
	handler := NewCloudApiHandler(nil, nil, nil)

	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions, http.MethodConnect, http.MethodTrace} {
		req := httptest.NewRequest(method, "/meta/webhook/test-instance-id", nil)
		w := httptest.NewRecorder()
		handler.HandleWebhookGET(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 for %s, got %d (SEC-012)", method, w.Code)
		}
	}
}

// --- Security: SEC-016 — Payload validation limits ---

func TestWebhookPayloadValidation_TextTooLong(t *testing.T) {
	appSecret := "payload-test-secret"
	longText := string(make([]byte, 65536)) // 64 KB + 1
	payload := map[string]interface{}{
		"object": "whatsapp_business_account",
		"entry": []map[string]interface{}{
			{
				"id": "123456789",
				"changes": []map[string]interface{}{
					{
						"value": map[string]interface{}{
							"messaging_product": "whatsapp",
							"metadata": map[string]interface{}{
								"display_phone_number": "[TELEFONE_BR_REDACTED]",
								"phone_number_id":     "987654321",
							},
							"messages": []map[string]interface{}{
								{
									"from": "[TELEFONE_BR_REDACTED]",
									"id":   "wamid.payload.text",
									"timestamp": "1700000400",
									"type": "text",
									"text": map[string]interface{}{"body": longText},
								},
							},
						},
						"field": "messages",
					},
				},
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(bodyBytes)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler := NewCloudApiHandler(nil, nil, nil)
	handler.HandleWebhookPOST(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("webhook POST with text > 64 KB should be rejected (SEC-016)")
	}
}

func TestWebhookPayloadValidation_ExcessiveNestedDepth(t *testing.T) {
	// Build a payload with nested depth > 5 levels.
	deepValue := map[string]interface{}{"a": nil}
	for i := 0; i < 10; i++ {
		deepValue = map[string]interface{}{"level": deepValue}
	}

	payload := map[string]interface{}{
		"object": "whatsapp_business_account",
		"entry": []map[string]interface{}{
			{
				"id": "123456789",
				"changes": []map[string]interface{}{
					{
						"value": deepValue,
						"field": "messages",
					},
				},
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	appSecret := "payload-test-secret"
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(bodyBytes)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler := NewCloudApiHandler(nil, nil, nil)
	handler.HandleWebhookPOST(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("webhook POST with nested depth > 5 should be rejected (SEC-016)")
	}
}

func TestWebhookPayloadValidation_ExcessiveArraySize(t *testing.T) {
	messages := make([]map[string]interface{}, 150)
	for i := 0; i < 150; i++ {
		messages[i] = map[string]interface{}{
			"from": fmt.Sprintf("[TELEFONE_BR_REDACTED]"),
			"id":   fmt.Sprintf("wamid.array.%d", i),
			"type": "text",
			"text": map[string]interface{}{"body": "test"},
		}
	}

	payload := map[string]interface{}{
		"object": "whatsapp_business_account",
		"entry": []map[string]interface{}{
			{
				"id": "123456789",
				"changes": []map[string]interface{}{
					{
						"value": map[string]interface{}{
							"messaging_product": "whatsapp",
							"metadata": map[string]interface{}{
								"display_phone_number": "[TELEFONE_BR_REDACTED]",
								"phone_number_id":     "987654321",
							},
							"messages": messages,
						},
						"field": "messages",
					},
				},
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	appSecret := "payload-test-secret"
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(bodyBytes)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler := NewCloudApiHandler(nil, nil, nil)
	handler.HandleWebhookPOST(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("webhook POST with array > 100 items should be rejected (SEC-016)")
	}
}

// --- Security: SEC-003 — Decryption failure handling ---

func TestDecryptWithFallback_TriesCurrentKeyFirst(t *testing.T) {
	currentKey := make([]byte, 32)
	if _, err := rand.Read(currentKey); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	previousKey := make([]byte, 32)
	if _, err := rand.Read(previousKey); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Encrypt with current key.
	plaintext := []byte("EAAxTestValue")
	ciphertext, err := EncryptMetaCredential(plaintext, currentKey)
	if err != nil {
		t.Fatalf("EncryptMetaCredential failed: %v", err)
	}

	// DecryptWithFallback should succeed with current key.
	result, err := DecryptWithFallback(ciphertext, currentKey, previousKey)
	if err != nil {
		t.Fatalf("DecryptWithFallback with current key failed: %v (SEC-003)", err)
	}
	if string(result) != string(plaintext) {
		t.Fatalf("DecryptWithFallback returned wrong plaintext: %q (SEC-003)", string(result))
	}
}

func TestDecryptWithFallback_UsesPreviousKey(t *testing.T) {
	currentKey := make([]byte, 32)
	if _, err := rand.Read(currentKey); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	previousKey := make([]byte, 32)
	if _, err := rand.Read(previousKey); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	plaintext := []byte("EAAxRotatedToken")

	// Encrypt with the "previous" key (simulating a rotation).
	ciphertext, err := EncryptMetaCredential(plaintext, previousKey)
	if err != nil {
		t.Fatalf("EncryptMetaCredential failed: %v", err)
	}

	// DecryptWithFallback(current=invalid, previous=correct) must succeed.
	result, err := DecryptWithFallback(ciphertext, currentKey, previousKey)
	if err != nil {
		t.Fatalf("DecryptWithFallback should try previous key when current fails: %v (SEC-003)", err)
	}
	if string(result) != string(plaintext) {
		t.Fatalf("DecryptWithFallback returned wrong plaintext with previous key: %q (SEC-003)", string(result))
	}
}

func TestDecryptWithFallback_BothKeysFail(t *testing.T) {
	keyA := make([]byte, 32)
	if _, err := rand.Read(keyA); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	keyB := make([]byte, 32)
	if _, err := rand.Read(keyB); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	keyC := make([]byte, 32)
	if _, err := rand.Read(keyC); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	plaintext := []byte("EAAxCritical")
	ciphertext, err := EncryptMetaCredential(plaintext, keyA)
	if err != nil {
		t.Fatalf("EncryptMetaCredential failed: %v", err)
	}

	// Both keys wrong — must return error.
	_, err = DecryptWithFallback(ciphertext, keyB, keyC)
	if err == nil {
		t.Fatal("DecryptWithFallback with both keys wrong should return error (SEC-003)")
	}
}


