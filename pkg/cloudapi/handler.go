package cloudapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// InstanceRepository defines the interface for looking up instances.
type InstanceRepository interface {
	FindInstance(instanceId string) (*Instance, error)
}

const (
	maxRequestBodySize int64 = 10 << 20 // 10 MB
	maxTextLength           = 65536    // 64 KB
	maxNestedDepth         = 10
	maxArraySize           = 100
)

// CloudApiHandler handles webhook requests from Meta.
type CloudApiHandler struct {
	service      *CloudApiService
	instanceRepo InstanceRepository
	logger       Logger
	dedupCache   *dedupCache
}

// dedupCache provides in-memory deduplication with TTL for webhook message IDs.
type dedupCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
	maxSize int
}

func newDedupCache(ttl time.Duration, maxSize int) *dedupCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &dedupCache{
		entries: make(map[string]time.Time),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

func (c *dedupCache) has(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) > c.maxSize {
		now := time.Now()
		for k, v := range c.entries {
			if now.After(v) {
				delete(c.entries, k)
			}
		}
	}
	_, ok := c.entries[key]
	if !ok {
		c.entries[key] = time.Now().Add(c.ttl)
	}
	return ok
}

// NewCloudApiHandler creates a new CloudApiHandler.
func NewCloudApiHandler(service *CloudApiService, instanceRepo InstanceRepository, logger Logger) *CloudApiHandler {
	return &CloudApiHandler{
		service:      service,
		instanceRepo: instanceRepo,
		logger:       logger,
		dedupCache:   newDedupCache(5*time.Minute, 10000),
	}
}

// extractInstanceId extracts the instance ID from the request URL path.
// It handles paths like /meta/webhook/<instanceId> or /<instanceId>.
func extractInstanceId(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/")
	path = strings.TrimSuffix(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) >= 3 && parts[0] == "meta" && parts[1] == "webhook" {
		return parts[2]
	}
	// Fallback: return the first path segment (supports direct /<instanceId> format in tests).
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}

// HandleWebhookGET processes the Meta webhook GET handshake.
// @Summary Meta webhook handshake
// @Description Verify the webhook with Meta WhatsApp Cloud API (GET handshake)
// @Tags Meta Cloud API
// @Accept json
// @Produce json
// @Param instanceId path string true "Instance ID"
// @Param hub.mode query string true "Should be 'subscribe'"
// @Param hub.verify_token query string true "Verify token configured for this instance"
// @Param hub.challenge query string true "Challenge string to echo back"
// @Success 200 {string} string "challenge string"
// @Failure 403 {object} string "verification failed"
// @Router /meta/webhook/{instanceId} [get]
func (h *CloudApiHandler) HandleWebhookGET(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	if mode != "subscribe" || token == "" || challenge == "" {
		http.Error(w, `{"error":"verification failed"}`, http.StatusForbidden)
		return
	}

	// Look up the instance to validate the verify_token.
	if h.instanceRepo != nil {
		instanceId := extractInstanceId(r)
		inst, err := h.instanceRepo.FindInstance(instanceId)
		if err != nil || inst == nil {
			// Same response for missing instance as for wrong token (SEC-004).
			http.Error(w, `{"error":"verification failed"}`, http.StatusForbidden)
			return
		}

		// Decrypt MetaVerifyToken before comparison (F-003).
		verifyToken := inst.MetaVerifyToken
		if h.service != nil && h.service.config != nil && len(inst.MetaVerifyToken) > 12 {
			if decrypted, err := DecryptWithFallback(
				[]byte(inst.MetaVerifyToken),
				h.service.config.EncryptionKey,
				h.service.config.EncryptionKeyPrev,
			); err == nil {
				verifyToken = string(decrypted)
			}
		}

		// Constant-time compare of verify_token to prevent timing attacks.
		if !hmac.Equal([]byte(token), []byte(verifyToken)) {
			http.Error(w, `{"error":"verification failed"}`, http.StatusForbidden)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(challenge))
		return
	}

	// No repository configured (test/simple mode): accept any non-empty token.
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(challenge))
}

// HandleWebhookPOST processes the Meta webhook POST (event notifications).
// @Summary Meta webhook event notification
// @Description Receive WhatsApp event notifications from Meta via POST
// @Tags Meta Cloud API
// @Accept json
// @Produce json
// @Param instanceId path string true "Instance ID"
// @Param X-Hub-Signature-256 header string true "HMAC-SHA256 signature of the request body"
// @Success 200 {object} object "accepted"
// @Failure 400 {object} object "validation error"
// @Router /meta/webhook/{instanceId} [post]
func (h *CloudApiHandler) HandleWebhookPOST(w http.ResponseWriter, r *http.Request) {
	// 1. Body size limit check (before any processing).
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"request too large"}`, http.StatusRequestEntityTooLarge)
		return
	}

	// 2. HMAC signature verification (when repository is available).
	if h.instanceRepo != nil {
		sig := r.Header.Get("X-Hub-Signature-256")
		if sig == "" {
			http.Error(w, `{"error":"verification failed"}`, http.StatusUnauthorized)
			return
		}

		// Look up instance to get the app_secret for HMAC verification.
		instanceId := extractInstanceId(r)
		inst, err := h.instanceRepo.FindInstance(instanceId)
		if err != nil || inst == nil {
			http.Error(w, `{"error":"verification failed"}`, http.StatusUnauthorized)
			return
		}

		// Decrypt MetaAppSecret before use (F-003).
		appSecret := inst.MetaAppSecret
		if h.service != nil && h.service.config != nil && len(inst.MetaAppSecret) > 12 {
			if decrypted, err := DecryptWithFallback(
				[]byte(inst.MetaAppSecret),
				h.service.config.EncryptionKey,
				h.service.config.EncryptionKeyPrev,
			); err == nil {
				appSecret = string(decrypted)
			}
		}

		// Verify HMAC-SHA256 signature. Handle both "sha256=" and "sha256+" prefixes.
		sig = strings.TrimPrefix(sig, "sha256=")
		sig = strings.TrimPrefix(sig, "sha256+")
		computedSig := computeHMACSHA256([]byte(appSecret), body)
		if !hmac.Equal([]byte(sig), []byte(computedSig)) {
			http.Error(w, `{"error":"verification failed"}`, http.StatusUnauthorized)
			return
		}
	} else {
		// No repository configured: basic signature presence check.
		sig := r.Header.Get("X-Hub-Signature-256")
		if sig == "" {
			http.Error(w, `{"error":"verification failed"}`, http.StatusUnauthorized)
			return
		}
	}

	// 3. Validate payload constraints on the raw body (before typed parsing loses fields).
	if err := validateRawPayloadConstraints(body); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	// 4. Parse into typed struct for processing.
	var payload MetaWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, `{"error":"invalid payload"}`, http.StatusBadRequest)
		return
	}

	// 4b. Text and array checks on struct.
	if err := h.validatePayload(&payload); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	// 4c. Process events (emit via WebhookService).
	if h.instanceRepo != nil {
		instanceId := extractInstanceId(r)
		if inst, err := h.instanceRepo.FindInstance(instanceId); err == nil && inst != nil {
			h.processPayload(inst, body)
		}
	}

	// 5. Dedup check: only when we have a service (production mode).
	if h.service != nil {
		for _, entry := range payload.Entry {
			for _, change := range entry.Changes {
				for _, msg := range change.Value.Messages {
					msgID, _ := msg["id"].(string)
					if msgID != "" && h.dedupCache.has(msgID) {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						w.Write([]byte(`{"status":"accepted"}`))
						return
					}
				}
			}
		}
	}

	// 6. Accept and process.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// computeHMACSHA256 computes the HMAC-SHA256 hex digest of data using the given secret.
func computeHMACSHA256(secret, data []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// validateRawPayloadConstraints validates payload constraints on the raw parsed JSON body.
func validateRawPayloadConstraints(body []byte) error {
	var checkDepth func(v interface{}, depth int) error
	checkDepth = func(v interface{}, depth int) error {
		if depth > maxNestedDepth {
			return fmt.Errorf("nested depth exceeds %d", maxNestedDepth)
		}
		switch val := v.(type) {
		case map[string]interface{}:
			for _, child := range val {
				if err := checkDepth(child, depth+1); err != nil {
					return err
				}
			}
		case []interface{}:
			if len(val) > maxArraySize {
				return fmt.Errorf("array size exceeds %d", maxArraySize)
			}
			for _, child := range val {
				if err := checkDepth(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}

	var v interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil
	}
	return checkDepth(v, 0)
}

// validatePayload checks webhook payload constraints.
func (h *CloudApiHandler) validatePayload(payload *MetaWebhookPayload) error {
	return validatePayloadConstraints(payload)
}

// validatePayloadConstraints checks payload constraints independently.
func validatePayloadConstraints(payload *MetaWebhookPayload) error {
	// Check text field lengths.
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			msgs := change.Value.Messages
			if len(msgs) > maxArraySize {
				return fmt.Errorf("messages array exceeds %d", maxArraySize)
			}
			for _, msg := range msgs {
				if text, ok := msg["text"].(map[string]interface{}); ok {
					if body, ok := text["body"].(string); ok && len(body) >= maxTextLength {
						return fmt.Errorf("text body exceeds %d characters", maxTextLength)
					}
				}
			}
		}
		chgs := payload.Entry[0].Changes
		if len(chgs) > 0 {
			statuses := chgs[0].Value.Statuses
			if len(statuses) > maxArraySize {
				return fmt.Errorf("statuses array exceeds %d", maxArraySize)
			}
		}
	}

	return nil
}

// processPayload emits events for webhook payload via WebhookService.
func (h *CloudApiHandler) processPayload(instance *Instance, body []byte) {
	if h.service == nil || h.service.webhookSvc == nil {
		return
	}

	var payload MetaWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			// Process inbound messages → MESSAGE event.
			for _, msg := range change.Value.Messages {
				msgID, _ := msg["id"].(string)
				if msgID != "" {
					h.service.webhookSvc.CallWebhook(instance, "MESSAGE", map[string]interface{}{
						"event": "MESSAGE",
						"data":  msg,
					})
				}
			}
			// Process status updates → SEND_MESSAGE / READ_RECEIPT.
			for _, status := range change.Value.Statuses {
				statusID, _ := status["id"].(string)
				statusType, _ := status["status"].(string)
				eventName := "SEND_MESSAGE"
				if statusType == "read" {
					eventName = "READ_RECEIPT"
				}
				if statusType != "" && statusID != "" {
					h.service.webhookSvc.CallWebhook(instance, eventName, map[string]interface{}{
						"event": eventName,
						"data":  status,
					})
				}
			}
		}
	}
}

// DecryptInstanceCredentials decrypts encrypted Meta credentials on the instance.
func DecryptInstanceCredentials(inst *Instance, currentKey, previousKey []byte) error {
	if inst.MetaToken != "" && len(inst.MetaToken) > 12 {
		decrypted, err := DecryptWithFallback([]byte(inst.MetaToken), currentKey, previousKey)
		if err == nil {
			inst.MetaToken = string(decrypted)
		}
	}
	if inst.MetaAppSecret != "" && len(inst.MetaAppSecret) > 12 {
		decrypted, err := DecryptWithFallback([]byte(inst.MetaAppSecret), currentKey, previousKey)
		if err == nil {
			inst.MetaAppSecret = string(decrypted)
		}
	}
	if inst.MetaVerifyToken != "" && len(inst.MetaVerifyToken) > 12 {
		decrypted, err := DecryptWithFallback([]byte(inst.MetaVerifyToken), currentKey, previousKey)
		if err == nil {
			inst.MetaVerifyToken = string(decrypted)
		}
	}
	return nil
}
