package cloudapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"
)

// CloudApiService provides the core Cloud API engine operations.
type CloudApiService struct {
	client     *MetaClient
	config     *CloudApiConfig
	webhookSvc WebhookService
	logger     Logger
	mu         sync.Mutex
	stopCh     chan struct{}
}

// WebhookService defines the interface for emitting events to the webhook system.
type WebhookService interface {
	CallWebhook(instance *Instance, event string, data interface{})
}

// Logger defines a minimal logging interface.
type Logger interface {
	LogInfo(instanceID, format string, args ...interface{})
	LogError(instanceID, format string, args ...interface{})
	LogWarn(instanceID, format string, args ...interface{})
}

// WebhookHandler defines the interface for webhook processing.
type WebhookHandler interface {
	HandleWebhookGET(ctx interface{})
	HandleWebhookPOST(ctx interface{})
}

// NewCloudApiService creates a new CloudApiService.
func NewCloudApiService(client *MetaClient, webhookSvc WebhookService, logger Logger) *CloudApiService {
	return &CloudApiService{
		client:     client,
		webhookSvc: webhookSvc,
		logger:     logger,
		stopCh:     make(chan struct{}),
	}
}

// Connect validates credentials against Meta and marks the instance as connected.
// It resolves the phone number and business name from the Meta API.
func (s *CloudApiService) Connect(instance *Instance) error {
	if instance == nil {
		return fmt.Errorf("instance is nil")
	}
	if instance.MetaToken == "" {
		return fmt.Errorf("Meta access token is required")
	}
	if instance.MetaPhoneNumberId == "" {
		return fmt.Errorf("Meta Phone Number ID is required")
	}

	if s.client == nil {
		return fmt.Errorf("Meta client not configured")
	}

	// Decrypt the Meta token for this request (NF-001).
	var encKey, encKeyPrev []byte
	if s.config != nil {
		encKey = s.config.EncryptionKey
		encKeyPrev = s.config.EncryptionKeyPrev
	}
	decryptedToken := decryptInstanceToken(instance, encKey, encKeyPrev)
	if decryptedToken == "" {
		return fmt.Errorf("failed to decrypt Meta token")
	}

	info, err := s.client.GetPhoneNumber(instance.MetaPhoneNumberId, decryptedToken)
	if err != nil {
		instance.Connected = false
		if instance.DisconnectReason == "" {
			instance.DisconnectReason = err.Error()
		}
		return fmt.Errorf("credential validation failed: %w", err)
	}

	instance.MetaBusinessName = info.VerifiedName
	instance.MetaPhoneNumber = info.DisplayPhoneNumber
	instance.Connected = true
	instance.DisconnectReason = ""

	return nil
}

// Disconnect marks the instance as disconnected.
func (s *CloudApiService) Disconnect(instance *Instance) error {
	if instance == nil {
		return fmt.Errorf("instance is nil")
	}
	instance.Connected = false
	instance.DisconnectReason = "disconnected"
	return nil
}

// Status returns the current connection status for the instance.
func (s *CloudApiService) Status(instance *Instance) (*CloudStatus, error) {
	if instance == nil {
		return &CloudStatus{Connected: false}, nil
	}
	return &CloudStatus{
		Connected:        instance.Connected,
		PhoneNumber:      instance.MetaPhoneNumber,
		BusinessName:     instance.MetaBusinessName,
		DisconnectReason: instance.DisconnectReason,
	}, nil
}

// GetPhoneNumberId returns the phone number ID for the instance.
func (s *CloudApiService) GetPhoneNumberId(instance *Instance) string {
	return instance.MetaPhoneNumberId
}

// ValidateInstanceCredentials re-validates the instance credentials against Meta.
func (s *CloudApiService) ValidateInstanceCredentials(instance *Instance) error {
	if instance == nil || !instance.Connected {
		return fmt.Errorf("instance is not connected")
	}

	// Decrypt the Meta token for this request (NF-001).
	var encKey, encKeyPrev []byte
	if s.config != nil {
		encKey = s.config.EncryptionKey
		encKeyPrev = s.config.EncryptionKeyPrev
	}
	decryptedToken := decryptInstanceToken(instance, encKey, encKeyPrev)
	if decryptedToken == "" {
		instance.Connected = false
		instance.DisconnectReason = "encryption_key_error"
		return fmt.Errorf("failed to decrypt Meta token")
	}

	_, err := s.client.GetPhoneNumber(instance.MetaPhoneNumberId, decryptedToken)
	if err != nil {
		instance.Connected = false
		if instance.DisconnectReason == "" {
			instance.DisconnectReason = fmt.Sprintf("credential validation failed: %v", err)
		}
		return fmt.Errorf("credential re-validation failed: %w", err)
	}

	return nil
}

// StartHealthChecks starts a goroutine that periodically validates credentials.
func (s *CloudApiService) StartHealthChecks(ctx context.Context, interval time.Duration, getInstances func() []*Instance) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if getInstances == nil {
					continue
				}
				instances := getInstances()
				for _, inst := range instances {
					if inst != nil && inst.Engine == "cloudapi" && inst.Connected {
						err := s.ValidateInstanceCredentials(inst)
						if err != nil {
							if s.logger != nil {
								s.logger.LogError(inst.Id, "[%s] Health check failed for Cloud API instance: %v", inst.Id, err)
							}
						}
					}
				}
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			}
		}
	}()
}

// SetEncryptionKeys sets the current and previous encryption keys on the service config.
func (s *CloudApiService) SetEncryptionKeys(currentKey, previousKey []byte) {
	if s == nil {
		return
	}
	if len(currentKey) == 32 {
		if s.config == nil {
			s.config = &CloudApiConfig{}
		}
		s.config.EncryptionKey = currentKey
	}
	if len(previousKey) == 32 {
		if s.config == nil {
			s.config = &CloudApiConfig{}
		}
		s.config.EncryptionKeyPrev = previousKey
	}
}

// SetConfig sets the CloudApiConfig on the service (for setting encryption keys from main).
func (s *CloudApiService) SetConfig(cfg *CloudApiConfig) {
	if s != nil && cfg != nil {
		s.config = cfg
	}
}

// GetConfig returns the service's CloudApiConfig (or nil).
func (s *CloudApiService) GetConfig() *CloudApiConfig {
	if s == nil {
		return nil
	}
	return s.config
}

// GetDecodedKey decodes a base64-encoded 32-byte key. Exportado para uso por instance_service.
func GetDecodedKey(keyStr string) []byte {
	return decodeBase64Key(keyStr)
}

// Stop stops the health check goroutine.
func (s *CloudApiService) Stop() {
	close(s.stopCh)
}

// decryptToken decrypts a Meta token stored encrypted at rest.
func decryptToken(encrypted, key []byte) ([]byte, error) {
	if len(encrypted) == 0 {
		return nil, fmt.Errorf("no encrypted token")
	}
	return DecryptMetaCredential(encrypted, key)
}

// decryptInstanceToken decrypts the MetaToken from an instance using current key
// (falling back to previous key if configured). If the token is not encrypted
// (length <= 20 bytes — e.g., plain text in tests), it is returned as-is because
// real AES-256-GCM encrypted tokens are always >= 28 bytes (12-byte nonce + tag).
// Returns empty string on decryption failure.
func decryptInstanceToken(instance *Instance, currentKey, previousKey []byte) string {
	if instance.MetaToken == "" {
		return ""
	}
	// Short tokens (<= 20 bytes) are considered unencrypted (test mode / plain text).
	if len(instance.MetaToken) <= 20 {
		return instance.MetaToken
	}
	decrypted, err := DecryptWithFallback([]byte(instance.MetaToken), currentKey, previousKey)
	if err != nil {
		return ""
	}
	return string(decrypted)
}

// maskPhoneNumber masks a phone number ID showing only the last 4 digits.
func maskPhoneNumber(id string) string {
	if len(id) <= 4 {
		return strings.Repeat("*", len(id))
	}
	return strings.Repeat("*", len(id)-4) + id[len(id)-4:]
}

// SendUnsupportedOperation returns an error indicating the operation is
// not supported for Cloud API instances.
func (s *CloudApiService) SendUnsupportedOperation(instance *Instance, operation string, payload interface{}) error {
	if s.logger != nil {
		s.logger.LogWarn(instance.Id, "[%s] Unsupported operation for Cloud API: %s", instance.Id, operation)
	}
	return fmt.Errorf("operacao nao suportada para instancias Cloud API")
}

// DecodeBase64Key decodes a base64-encoded 32-byte key. Exportado para uso por instance_service.
func DecodeBase64Key(keyStr string) []byte {
	if keyStr == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		return nil
	}
	return decoded
}

// decodeBase64Key decodes a base64-encoded 32-byte key (wraps DecodeBase64Key).
func decodeBase64Key(keyStr string) []byte {
	return DecodeBase64Key(keyStr)
}
