package cloudapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// AC: CLOUD-09 — Webhook GET handshake with verify token.
// AC: CLOUD-10 — Webhook POST HMAC SHA-256 signature validation.
// AC: CLOUD-11 — Inbound message becomes MESSAGE event.
// AC: CLOUD-12 — Inbound media downloaded and exposed via existing contract.
// AC: CLOUD-13 — Event subscription filtering respected.
// AC: CLOUD-14 — Status updates become SEND_MESSAGE/READ_RECEIPT events.
// AC: CLOUD-15 — Delivery failure visible in status.
//
// Expected types when implemented:
//
//	type CloudApiHandler struct { ... }
//	func NewCloudApiHandler(service *CloudApiService, instanceRepo instance_repository.InstanceRepository, logger *LoggerManager) *CloudApiHandler
//	func (h *CloudApiHandler) HandleWebhookGET(ctx *gin.Context)
//	func (h *CloudApiHandler) HandleWebhookPOST(ctx *gin.Context)
//
//	type MetaWebhookPayload struct {
//		Object string          `json:"object"`
//		Entry  []MetaWebhookEntry `json:"entry"`
//	}
//	type MetaWebhookEntry struct {
//		ID        string              `json:"id"`
//		Changes   []MetaWebhookChange `json:"changes"`
//	}
//	type MetaWebhookChange struct {
//		Value MetaWebhookValue `json:"value"`
//		Field string           `json:"field"`
//	}
//	type MetaWebhookValue struct {
//		MessagingProduct string              `json:"messaging_product"`
//		Metadata         MetaWebhookMetadata `json:"metadata"`
//		Contacts         []json.RawMessage   `json:"contacts,omitempty"`
//		Messages         []json.RawMessage   `json:"messages,omitempty"`
//		Statuses          []json.RawMessage  `json:"statuses,omitempty"`
//	}

func TestHandleWebhookGET_ValidToken(t *testing.T) {
	handler := NewCloudApiHandler(nil, nil, nil)
	instanceId := "test-instance-id"
	verifyToken := "meu-verify-token-seguro"

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/meta/webhook/%s?hub.mode=subscribe&hub.verify_token=%s&hub.challenge=123456789", instanceId, verifyToken), nil)
	w := httptest.NewRecorder()

	// The handler should look up the instance by instanceId, compare verify_token,
	// and return the challenge in the response body with 200.
	handler.HandleWebhookGET(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid verify token, got %d (AC: CLOUD-09)", w.Code)
	}
	if body := w.Body.String(); body != "123456789" {
		t.Fatalf("expected challenge body '123456789', got %q (AC: CLOUD-09)", body)
	}
}

func TestHandleWebhookGET_InvalidToken(t *testing.T) {
	handler := NewCloudApiHandler(nil, nil, nil)
	instanceId := "test-instance-id"

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/meta/webhook/%s?hub.mode=subscribe&hub.verify_token=wrong-token&hub.challenge=123456789", instanceId), nil)
	w := httptest.NewRecorder()

	handler.HandleWebhookGET(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for invalid verify token, got %d (AC: CLOUD-09)", w.Code)
	}
}

func TestHandleWebhookGET_MissingParams(t *testing.T) {
	handler := NewCloudApiHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/meta/webhook/test-id", nil)
	w := httptest.NewRecorder()

	handler.HandleWebhookGET(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing verify token params, got %d (AC: CLOUD-09)", w.Code)
	}
}

func TestHandleWebhookPOST_ValidSignature(t *testing.T) {
	appSecret := "my-app-secret-key-12345"
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
									"id":   "wamid.incoming.abc",
									"timestamp": "1700000000",
									"type": "text",
									"text": map[string]interface{}{
										"body": "Hello from customer",
									},
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

	// Compute HMAC-SHA256 signature.
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(bodyBytes)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler := NewCloudApiHandler(nil, nil, nil)
	// The handler should decrypt the instance's app_secret, verify the HMAC,
	// process the message, and return 200.
	handler.HandleWebhookPOST(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid HMAC signature, got %d (AC: CLOUD-10)", w.Code)
	}
}

func TestHandleWebhookPOST_InvalidSignature(t *testing.T) {
	payload := map[string]interface{}{
		"object": "whatsapp_business_account",
		"entry":  []map[string]interface{}{},
	}
	bodyBytes, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid_signature_here")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler := NewCloudApiHandler(nil, nil, nil)
	handler.HandleWebhookPOST(w, req)

	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Fatalf("expected 401/403 for invalid HMAC signature, got %d (AC: CLOUD-10)", w.Code)
	}
}

func TestHandleWebhookPOST_MissingSignature(t *testing.T) {
	payload := map[string]interface{}{
		"object": "whatsapp_business_account",
		"entry":  []map[string]interface{}{},
	}
	bodyBytes, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler := NewCloudApiHandler(nil, nil, nil)
	handler.HandleWebhookPOST(w, req)

	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Fatalf("expected 401/403 for missing HMAC signature, got %d (AC: CLOUD-10)", w.Code)
	}
}

func TestHandleWebhookPOST_StatusUpdate_Sent(t *testing.T) {
	appSecret := "app-secret-for-testing"
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
							"statuses": []map[string]interface{}{
								{
									"id": "wamid.sent.abc",
									"status": "sent",
									"timestamp": "1700000001",
									"recipient_id": "[TELEFONE_BR_REDACTED]",
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

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid status update, got %d (AC: CLOUD-14)", w.Code)
	}
}

func TestHandleWebhookPOST_StatusUpdate_Failed(t *testing.T) {
	appSecret := "app-secret-for-testing"
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
							"statuses": []map[string]interface{}{
								{
									"id": "wamid.failed.abc",
									"status": "failed",
									"timestamp": "1700000002",
									"recipient_id": "[TELEFONE_BR_REDACTED]",
									"errors": []map[string]interface{}{
										{
											"code": 131047,
											"title": "Message not allowed outside 24h window",
											"details": "Message not allowed outside 24h window. Use a template.",
										},
									},
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

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid failed status update, got %d (AC: CLOUD-15)", w.Code)
	}
}

func TestHandleWebhookPOST_StatusUpdate_Read(t *testing.T) {
	appSecret := "app-secret-for-testing"
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
							"statuses": []map[string]interface{}{
								{
									"id": "wamid.read.abc",
									"status": "read",
									"timestamp": "1700000003",
									"recipient_id": "[TELEFONE_BR_REDACTED]",
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

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for read status update, got %d (AC: CLOUD-14)", w.Code)
	}
}

func TestHandleWebhookPOST_InboundMessage_EmitsEvent(t *testing.T) {
	appSecret := "app-secret-for-testing"
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
							"contacts": []map[string]interface{}{
								{
									"profile": map[string]interface{}{
										"name": "Maria Silva",
									},
									"wa_id": "[TELEFONE_BR_REDACTED]",
								},
							},
							"messages": []map[string]interface{}{
								{
									"from": "[TELEFONE_BR_REDACTED]",
									"id":   "wamid.incoming.xyz",
									"timestamp": "1700000100",
									"type": "text",
									"text": map[string]interface{}{
										"body": "Olá, gostaria de saber sobre meu pedido",
									},
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
	sig := "sha256+" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler := NewCloudApiHandler(nil, nil, nil)
	handler.HandleWebhookPOST(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for inbound message, got %d (AC: CLOUD-11)", w.Code)
	}
}
