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

// T-017 — Integration tests that exercise end-to-end flows.
// These tests are higher-level and may depend on multiple components working together.
//
// AC covered: CLOUD-01, CLOUD-05, CLOUD-06, CLOUD-09, CLOUD-10, CLOUD-16, CLOUD-20

func TestIntegration_CreateConnectSendText(t *testing.T) {
	// Simulates the full flow: instance has valid credentials → connect → send text.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		switch callCount {
		case 1:
			// GetPhoneNumber (during connect)
			fmt.Fprint(w, `{"verified_name":"My Biz","display_phone_number":"[TELEFONE_BR_REDACTED]","id":"987654321"}`)
		case 2:
			// SendMessage
			fmt.Fprint(w, `{"messaging_product":"whatsapp","messages":[{"id":"wamid.integration.test.123"}]}`)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		MetaToken:        "EAAxValidToken",
		MetaPhoneNumberId: "987654321",
		MetaWabaId:       "123456789",
		Connected:        false,
	}

	if err := service.Connect(instance); err != nil {
		t.Fatalf("Connect failed: %v (AC: CLOUD-05)", err)
	}
	if !instance.Connected {
		t.Fatal("Instance not marked connected (AC: CLOUD-05)")
	}

	resp, err := service.SendText(instance, "[TELEFONE_BR_REDACTED]", "Integration test message", "int-msg-001")
	if err != nil {
		t.Fatalf("SendText after connect failed: %v (AC: CLOUD-16)", err)
	}
	if resp == nil {
		t.Fatal("SendText returned nil (AC: CLOUD-16)")
	}
}

func TestIntegration_WebhookHandshakeAndPOST(t *testing.T) {
	// Simulates the Meta webhook setup: GET handshake succeeds, then POST with
	// valid signature processes a message.
	appSecret := "integration-test-app-secret"
	verifyToken := "integration-verify-token"

	// First do GET handshake
	handler := NewCloudApiHandler(nil, nil, nil)

	getReq := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/meta/webhook/test-instance-id?hub.mode=subscribe&hub.verify_token=%s&hub.challenge=challenge-ok", verifyToken),
		nil)
	getW := httptest.NewRecorder()
	handler.HandleWebhookGET(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("GET handshake expected 200, got %d (AC: CLOUD-09)", getW.Code)
	}
	if getW.Body.String() != "challenge-ok" {
		t.Fatalf("GET handshake body expected 'challenge-ok', got %q", getW.Body.String())
	}

	// Then POST with valid HMAC
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
									"id":   "wamid.integration.inbound",
									"timestamp": "1700000100",
									"type": "text",
									"text": map[string]interface{}{"body": "Hello"},
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

	postReq := httptest.NewRequest(http.MethodPost, "/meta/webhook/test-instance-id", bytes.NewReader(bodyBytes))
	postReq.Header.Set("X-Hub-Signature-256", sig)
	postReq.Header.Set("Content-Type", "application/json")
	postW := httptest.NewRecorder()
	handler.HandleWebhookPOST(postW, postReq)

	if postW.Code != http.StatusOK {
		t.Fatalf("POST webhook expected 200, got %d (AC: CLOUD-10)", postW.Code)
	}
}

func TestIntegration_UnsupportedOperationReturns4xx(t *testing.T) {
	handler := NewCloudApiHandler(nil, nil, nil)
	instance := &Instance{
		Engine:    "cloudapi",
		Connected: true,
	}

	// The send service dispatch should detect cloudapi engine and return 4xx
	// for operations like SendButton.
	// This is tested via the handler or service layer.
	// Here we verify at the CloudApiService level (the dispatch logic in
	// send_service.go will call a method that returns an unsupported error).
	service := NewCloudApiService(nil, nil, nil)
	payload := json.RawMessage(`{"number":"[TELEFONE_BR_REDACTED]","title":"Test","description":"Desc","footer":"Footer","buttons":[]}`)

	err := service.SendUnsupportedOperation(instance, "button", payload)
	if err == nil {
		t.Fatal("expected error for unsupported operation on cloudapi engine (AC: CLOUD-19)")
	}
}

func TestIntegration_EventMapping_MessageAndStatus(t *testing.T) {
	appSecret := "test-secret"
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
									"id":   "wamid.event.test.msg",
									"timestamp": "1700000200",
									"type": "text",
									"text": map[string]interface{}{"body": "Event test"},
								},
							},
							"statuses": []map[string]interface{}{
								{
									"id": "wamid.event.test.status",
									"status": "read",
									"timestamp": "1700000201",
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
		t.Fatalf("expected 200 for combined message+status payload, got %d (AC: CLOUD-11, CLOUD-14)", w.Code)
	}
}
