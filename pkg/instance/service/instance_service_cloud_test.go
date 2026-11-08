package instance_service

import (
	"testing"

	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
)

// AC: CLOUD-01 — Cloud instance creation with valid credentials.
// AC: CLOUD-02 — Incomplete credentials rejected (400).
// AC: CLOUD-03 — Duplicate name still forbidden (cross-engine).
// AC: CLOUD-04 — Coexistence without regression (whatsmeow unchanged).
// AC: CLOUD-05 — Connect dispatches to CloudApiService for cloudapi engine.
// AC: CLOUD-06 — Invalid credentials do not connect.
// AC: CLOUD-07 — GetQr returns 4xx for cloudapi engine.
// AC: CLOUD-08 — Disconnect/logout for cloudapi engine.
//
// These tests expect the Instance model to include the new Engine and Meta
// credential fields (T-001) and the InstanceService to accept cloudApiService.

func TestCreate_CloudEngine_ValidCredentials(t *testing.T) {
	// When engine == "cloudapi", Create should:
	// 1. Accept and persist the Meta credential fields
	// 2. Encrypt MetaToken, MetaAppSecret, MetaVerifyToken at rest
	// 3. Mask MetaPhoneNumberId in response (masked version)
	// 4. Set Connected = false and Engine = "cloudapi"

	createData := &CreateStruct{
		Name:       "minha-cloud-instancia",
		Engine:     "cloudapi",
		MetaWabaId: "123456789",
		MetaPhoneNumberId: "987654321",
		MetaToken:  "EAAxTestAccessToken",
		MetaAppSecret: "my-app-secret",
		MetaVerifyToken: "my-verify-token",
	}

	// This will fail at compile time until T-001/T-006 add the fields.
	instance, err := instanceService.Create(createData)
	if err != nil {
		t.Fatalf("Create with valid cloud credentials should succeed, got: %v (AC: CLOUD-01)", err)
	}
	if instance == nil {
		t.Fatal("Create returned nil instance (AC: CLOUD-01)")
	}
	if instance.Engine != "cloudapi" {
		t.Fatalf("expected Engine='cloudapi', got %q (AC: CLOUD-01)", instance.Engine)
	}
	if instance.Connected {
		t.Fatal("New cloud instance should start disconnected (AC: CLOUD-01)")
	}
}

func TestCreate_CloudEngine_MissingFields(t *testing.T) {
	// Missing MetaToken should return 400 with clear indication of missing fields.

	createData := &CreateStruct{
		Name:   "cloud-sem-token",
		Engine: "cloudapi",
		// Missing: MetaToken, MetaAppSecret, MetaVerifyToken, MetaPhoneNumberId
		MetaWabaId: "123456789",
	}

	_, err := instanceService.Create(createData)
	if err == nil {
		t.Fatal("Create with missing cloud fields should return error (AC: CLOUD-02)")
	}
}

func TestCreate_DuplicateName_AcrossEngines(t *testing.T) {
	// Creating an instance with the same name as an existing one (any engine)
	// must fail with "instance already exists".

	createData := &CreateStruct{
		Name:   "existing-instance", // Assume this name already exists
		Engine: "cloudapi",
		MetaWabaId: "123456789",
		MetaPhoneNumberId: "987654321",
		MetaToken:  "EAAxToken",
		MetaAppSecret: "secret",
		MetaVerifyToken: "verify",
	}

	_, err := instanceService.Create(createData)
	if err == nil || err.Error() != "instance already exists" {
		t.Fatalf("expected 'instance already exists' error, got: %v (AC: CLOUD-03)", err)
	}
}

func TestConnect_CloudEngine(t *testing.T) {
	instance := &instance_model.Instance{
		Id:               "test-cloud-connect",
		Name:             "cloud-connect-test",
		Engine:           "cloudapi",
		MetaToken:        "EAAxValidToken",
		MetaPhoneNumberId: "987654321",
		Connected:        false,
	}

	connectData := &ConnectStruct{
		WebhookUrl:      "https://example.com/webhook",
		Subscribe:       []string{"MESSAGE"},
		Phone:           "",
	}

	_, _, _, err := instanceService.Connect(connectData, instance)
	if err != nil {
		t.Fatalf("Connect for cloudapi engine should succeed, got: %v (AC: CLOUD-05)", err)
	}
}

func TestConnect_CloudEngine_InvalidCredentials(t *testing.T) {
	instance := &instance_model.Instance{
		Id:               "test-cloud-invalid",
		Engine:           "cloudapi",
		MetaToken:        "EAAxInvalidToken",
		MetaPhoneNumberId: "987654321",
		Connected:        false,
	}

	connectData := &ConnectStruct{
		WebhookUrl: "https://example.com/webhook",
		Subscribe:  []string{"MESSAGE"},
	}

	_, _, _, err := instanceService.Connect(connectData, instance)
	if err == nil {
		t.Fatal("Connect with invalid token should return error (AC: CLOUD-06)")
	}
	if instance.Connected {
		t.Fatal("Instance should remain disconnected after failed connect (AC: CLOUD-06)")
	}
}

func TestGetQr_CloudEngine_Returns4xx(t *testing.T) {
	instance := &instance_model.Instance{
		Id:     "test-cloud-qr",
		Engine: "cloudapi",
	}

	_, err := instanceService.GetQr(instance)
	if err == nil {
		t.Fatal("GetQr for cloudapi should return error 'não suportado para instâncias Cloud API' (AC: CLOUD-07)")
	}
}

func TestPair_CloudEngine_Returns4xx(t *testing.T) {
	instance := &instance_model.Instance{
		Id:     "test-cloud-pair",
		Engine: "cloudapi",
	}

	pairData := &PairStruct{
		Phone: "[TELEFONE_BR_REDACTED]",
	}

	_, err := instanceService.Pair(pairData, instance)
	if err == nil {
		t.Fatal("Pair for cloudapi should return error 'não suportado para instâncias Cloud API' (AC: CLOUD-07)")
	}
}

func TestDisconnect_CloudEngine(t *testing.T) {
	instance := &instance_model.Instance{
		Id:        "test-cloud-disc",
		Engine:    "cloudapi",
		Connected: true,
	}

	_, err := instanceService.Disconnect(instance)
	if err != nil {
		t.Fatalf("Disconnect for cloudapi should succeed, got: %v (AC: CLOUD-08)", err)
	}
	if instance.Connected {
		t.Fatal("Disconnect should set Connected = false (AC: CLOUD-08)")
	}
}

func TestStatus_CloudEngine_NoQR(t *testing.T) {
	instance := &instance_model.Instance{
		Id:              "test-cloud-status",
		Engine:          "cloudapi",
		Connected:       true,
		MetaPhoneNumber: "[TELEFONE_BR_REDACTED]",
	}

	status, err := instanceService.Status(instance)
	if err != nil {
		t.Fatalf("Status for cloudapi should not require QR, got: %v (AC: CLOUD-07)", err)
	}
	if status == nil {
		t.Fatal("Status returned nil (AC: CLOUD-07)")
	}
}
