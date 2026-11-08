package cloudapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// AC: CLOUD-05 — Connect validates credentials and resolves phone number.
// AC: CLOUD-06 — Invalid credentials do not connect.
// AC: CLOUD-07 — Status/info works without QR for Cloud instances.
// AC: CLOUD-08 — Disconnect/logout deactivates the instance.
//
// Expected types when implemented:
//
//	type CloudApiService struct { ... }
//	func NewCloudApiService(...) *CloudApiService
//	func (s *CloudApiService) Connect(instance *Instance) error
//	func (s *CloudApiService) Disconnect(instance *Instance) error
//	func (s *CloudApiService) Status(instance *Instance) (*CloudStatus, error)
//	func (s *CloudApiService) GetPhoneNumberId(instance *Instance) string
//
//	type CloudStatus struct {
//		Connected        bool
//		PhoneNumber      string
//		BusinessName     string
//		DisconnectReason string
//	}
//
// Note: "Instance" here refers to instance_model.Instance with the new Engine
// and Meta credential fields added by T-001.

func TestCloudApiService_Connect_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"verified_name": "My Business Inc.",
			"display_phone_number": "[TELEFONE_BR_REDACTED]",
			"id": "987654321",
			"quality_rating": "GREEN",
			"code_verification_status": "VERIFIED"
		}`)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:        "cloudapi",
		MetaWabaId:    "123456789",
		MetaToken:     "EAAxValidToken",
		MetaPhoneNumberId: "987654321",
		Connected:     false,
	}

	err := service.Connect(instance)
	if err != nil {
		t.Fatalf("Connect with valid credentials should succeed, got: %v", err)
	}

	if !instance.Connected {
		t.Fatal("Connect should set instance.Connected = true (AC: CLOUD-05)")
	}
	if instance.MetaPhoneNumber == "" {
		t.Fatal("Connect should populate MetaPhoneNumber from Graph API response (AC: CLOUD-05)")
	}
	if instance.MetaBusinessName == "" {
		t.Fatal("Connect should populate MetaBusinessName from Graph API response (AC: CLOUD-05)")
	}
}

func TestCloudApiService_Connect_InvalidToken(t *testing.T) {
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
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:        "cloudapi",
		MetaWabaId:    "123456789",
		MetaToken:     "EAAxInvalidToken",
		MetaPhoneNumberId: "987654321",
		Connected:     false,
	}

	err := service.Connect(instance)
	if err == nil {
		t.Fatal("Connect with invalid token should return an error (AC: CLOUD-06)")
	}

	if instance.Connected {
		t.Fatal("Instance should remain disconnected after failed connect (AC: CLOUD-06)")
	}
	if instance.DisconnectReason == "" {
		t.Fatal("Connect failure should populate DisconnectReason (AC: CLOUD-06)")
	}
}

func TestCloudApiService_Connect_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "(#100) Invalid parameter",
				"code":    100,
			},
		})
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:        "cloudapi",
		MetaWabaId:    "000000000",
		MetaToken:     "EAAxToken",
		MetaPhoneNumberId: "000000000",
	}

	err := service.Connect(instance)
	if err == nil {
		t.Fatal("Connect with invalid WABA should error (AC: CLOUD-06)")
	}
}

func TestCloudApiService_Disconnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:    "cloudapi",
		Connected: true,
		MetaToken: "EAAxToken",
	}

	err := service.Disconnect(instance)
	if err != nil {
		t.Fatalf("Disconnect should succeed, got: %v", err)
	}

	if instance.Connected {
		t.Fatal("Disconnect should set Connected = false (AC: CLOUD-08)")
	}
}

func TestCloudApiService_Status_Connected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"verified_name": "Biz", "display_phone_number": "[TELEFONE_BR_REDACTED]", "id": "987654321"}`)
	}))
	defer srv.Close()

	client := NewMetaClient(srv.URL, "", srv.Client())
	service := NewCloudApiService(client, nil, nil)

	instance := &Instance{
		Engine:         "cloudapi",
		Connected:      true,
		MetaPhoneNumber: "[TELEFONE_BR_REDACTED]",
		MetaToken:      "EAAxToken",
	}

	status, err := service.Status(instance)
	if err != nil {
		t.Fatalf("Status should succeed, got: %v", err)
	}
	if status == nil {
		t.Fatal("Status returned nil")
	}
	if !status.Connected {
		t.Fatal("Status.Connected should be true for a connected instance (AC: CLOUD-07)")
	}
	if status.PhoneNumber != "[TELEFONE_BR_REDACTED]" {
		t.Fatal("Status.PhoneNumber should match instance MetaPhoneNumber (AC: CLOUD-07)")
	}
}

func TestCloudApiService_Status_Disconnected(t *testing.T) {
	service := NewCloudApiService(nil, nil, nil)

	instance := &Instance{
		Engine:    "cloudapi",
		Connected: false,
	}

	status, err := service.Status(instance)
	if err != nil {
		t.Fatalf("Status should not error for disconnected instance, got: %v", err)
	}
	if status.Connected {
		t.Fatal("Status.Connected should be false for disconnected instance (AC: CLOUD-07)")
	}
}
