package cloudapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// AC: CLOUD-16 — Send text within 24h window.
// AC: CLOUD-17 — Send media within 24h window.
// AC: CLOUD-18 — 24h window closed blocks free-form with clear error.
// AC: CLOUD-20 — Send approved template.
// AC: CLOUD-21 — Template not found/not approved fails with reason.
// AC: CLOUD-22 — Template parameters render correctly.
//
// Expected types when implemented:
//
//	type TemplateMessage struct {
//		Name       string             `json:"name"`
//		Language   string             `json:"language"`
//		Components []TemplateComponent `json:"components"`
//	}
//	type TemplateComponent struct {
//		Type       string              `json:"type"`
//		Parameters []TemplateParameter `json:"parameters"`
//	}
//	type TemplateParameter struct {
//		Type string `json:"type"`
//		Text string `json:"text,omitempty"`
//	}

func TestCloudApiService_SendText_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"messaging_product": "whatsapp",
			"messages": [{"id": "wamid.HBgNNTU0OTkyMTI5NTc5NRUCABIYFjNBMzk1MkZERTU5Rjc5QkUyQ0FGRTIA"}]
		}`)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	resp, err := service.SendText(instance, "[TELEFONE_BR_REDACTED]", "Hello from test", "msg-001")
	if err != nil {
		t.Fatalf("SendText should succeed within window, got: %v (AC: CLOUD-16)", err)
	}
	if resp == nil {
		t.Fatal("SendText returned nil response (AC: CLOUD-16)")
	}
}

func TestCloudApiService_SendText_Disconnected(t *testing.T) {
	service := NewCloudApiService(nil, nil, nil)

	instance := &Instance{
		Engine:    "cloudapi",
		Connected: false,
	}

	_, err := service.SendText(instance, "[TELEFONE_BR_REDACTED]", "Hello", "msg-001")
	if err == nil {
		t.Fatal("SendText on disconnected instance should return error (AC: CLOUD-08)")
	}
}

func TestCloudApiService_SendMedia_Success(t *testing.T) {
	// Mock two endpoints: upload media + send message
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			// Upload media response
			fmt.Fprint(w, `{"id":"media-id-12345"}`)
		} else {
			// Send message response
			fmt.Fprint(w, `{
				"messaging_product": "whatsapp",
				"messages": [{"id": "wamid.media.abc123"}]
			}`)
		}
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	resp, err := service.SendMedia(instance, "[TELEFONE_BR_REDACTED]", "image", "Test caption", "photo.jpg", []byte{0xFF, 0xD8, 0xFF})
	if err != nil {
		t.Fatalf("SendMedia should succeed, got: %v (AC: CLOUD-17)", err)
	}
	if resp == nil {
		t.Fatal("SendMedia returned nil response (AC: CLOUD-17)")
	}
}

func TestCloudApiService_SendTemplate_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"messaging_product": "whatsapp",
			"messages": [{"id": "wamid.tpl.abc123"}]
		}`)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	tmpl := &TemplateMessage{
		Name:     "aviso_cobranca",
		Language: "pt_BR",
		Components: []TemplateComponent{
			{
				Type: "body",
				Parameters: []TemplateParameter{
					{Type: "text", Text: "João"},
					{Type: "text", Text: "R$ 150,00"},
				},
			},
		},
	}

	resp, err := service.SendTemplate(instance, "[TELEFONE_BR_REDACTED]", tmpl)
	if err != nil {
		t.Fatalf("SendTemplate should succeed, got: %v (AC: CLOUD-20)", err)
	}
	if resp == nil {
		t.Fatal("SendTemplate returned nil response (AC: CLOUD-20)")
	}
}

func TestCloudApiService_SendTemplate_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "(#100) Template not found",
				"code":    100,
			},
		})
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	tmpl := &TemplateMessage{
		Name:     "nonexistent_template",
		Language: "pt_BR",
		Components: []TemplateComponent{
			{Type: "body", Parameters: []TemplateParameter{{Type: "text", Text: "test"}}},
		},
	}

	_, err := service.SendTemplate(instance, "[TELEFONE_BR_REDACTED]", tmpl)
	if err == nil {
		t.Fatal("SendTemplate with non-existent template should return error (AC: CLOUD-21)")
	}
}

func TestCloudApiService_SendTemplate_NotApproved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "(#132005) Template is not approved",
				"code":    132005,
			},
		})
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	tmpl := &TemplateMessage{
		Name:     "pending_template",
		Language: "pt_BR",
		Components: []TemplateComponent{
			{Type: "body", Parameters: []TemplateParameter{{Type: "text", Text: "test"}}},
		},
	}

	_, err := service.SendTemplate(instance, "[TELEFONE_BR_REDACTED]", tmpl)
	if err == nil {
		t.Fatal("SendTemplate with unapproved template should return error (AC: CLOUD-21)")
	}
}

func TestCloudApiService_SendText_Outside24hWindow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "(#131047) Cannot send message outside 24h window",
				"code":    131047,
				"error_data": map[string]interface{}{
					"details": "Message not allowed outside 24h window. Use a template.",
				},
			},
		})
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	_, err := service.SendText(instance, "[TELEFONE_BR_REDACTED]", "Hello", "msg-001")
	if err == nil {
		t.Fatal("SendText outside 24h window should return error (AC: CLOUD-18)")
	}
}

// --- Security: SEC-009 — MIME validation ---

func TestMimeValidation_ExecutableRejected(t *testing.T) {
	service := NewCloudApiService(nil, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	// Simulate an executable file uploaded as "document".
	exeData := []byte{0x4D, 0x5A, 0x90, 0x00} // MZ header (.exe)
	_, err := service.SendMedia(instance, "[TELEFONE_BR_REDACTED]", "document", "Caption", "malware.exe", exeData)
	if err == nil {
		t.Fatal("SendMedia with executable content should be rejected (SEC-009)")
	}
}

func TestMimeValidation_ImageTypeAcceptsJPEG(t *testing.T) {
	service := NewCloudApiService(nil, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	// JPEG header.
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}
	_, err := service.SendMedia(instance, "[TELEFONE_BR_REDACTED]", "image", "Test", "photo.jpg", jpegData)
	if err != nil {
		t.Fatalf("SendMedia with JPEG content should be accepted, got: %v (SEC-009)", err)
	}
}

func TestMimeValidation_PDFDocumentAccepted(t *testing.T) {
	service := NewCloudApiService(nil, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	pdfData := []byte{0x25, 0x50, 0x44, 0x46, 0x2D} // %PDF-
	_, err := service.SendMedia(instance, "[TELEFONE_BR_REDACTED]", "document", "Doc", "doc.pdf", pdfData)
	if err != nil {
		t.Fatalf("SendMedia with PDF content should be accepted, got: %v (SEC-009)", err)
	}
}

func TestMimeValidation_DeclaredTypeMismatch(t *testing.T) {
	service := NewCloudApiService(nil, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	// PNG image data declared as "video".
	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	_, err := service.SendMedia(instance, "[TELEFONE_BR_REDACTED]", "video", "Test", "image.png", pngData)
	if err == nil {
		t.Fatal("SendMedia with image content declared as 'video' should be rejected (SEC-009)")
	}
}

func TestMimeValidation_ShellScriptRejected(t *testing.T) {
	service := NewCloudApiService(nil, nil, nil)

	instance := &Instance{
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	scriptData := []byte("#!/bin/sh\necho 'malicious'")
	_, err := service.SendMedia(instance, "[TELEFONE_BR_REDACTED]", "document", "Script", "script.sh", scriptData)
	if err == nil {
		t.Fatal("SendMedia with shell script should be rejected (SEC-009)")
	}
}
