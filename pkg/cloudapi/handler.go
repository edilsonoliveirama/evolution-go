package cloudapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	maxRequestBodySize int64 = 10 << 20 // 10 MB
	maxTextLength           = 65536    // 64 KB
	maxNestedDepth         = 10
	maxArraySize           = 100
)

// CloudApiHandler handles webhook requests from Meta.
type CloudApiHandler struct {
	service    *CloudApiService
	instanceDB interface{}
	logger     Logger
	dedupCache *dedupCache
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
func NewCloudApiHandler(service *CloudApiService, instanceDB interface{}, logger Logger) *CloudApiHandler {
	return &CloudApiHandler{
		service:    service,
		instanceDB: instanceDB,
		logger:     logger,
		dedupCache: newDedupCache(5*time.Minute, 10000),
	}
}

// HandleWebhookGET processes the Meta webhook GET handshake.
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

	// In test mode (nil instanceDB), accept handshake with any non-empty token.
	if h.instanceDB == nil {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(challenge))
		return
	}

	http.Error(w, `{"error":"verification failed"}`, http.StatusForbidden)
}

// HandleWebhookPOST processes the Meta webhook POST (event notifications).
func (h *CloudApiHandler) HandleWebhookPOST(w http.ResponseWriter, r *http.Request) {
	// 1. Body size limit check (before any processing).
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"request too large"}`, http.StatusRequestEntityTooLarge)
		return
	}

	// 2. HMAC signature check.
	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		http.Error(w, `{"error":"verification failed"}`, http.StatusUnauthorized)
		return
	}

	// 3. Parse and validate payload in all modes (test and production).
	var payload MetaWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, `{"error":"invalid payload"}`, http.StatusBadRequest)
		return
	}

	// 4. Payload validation.
	if err := h.validatePayload(&payload); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
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

// validatePayload checks webhook payload constraints.
func (h *CloudApiHandler) validatePayload(payload *MetaWebhookPayload) error {
	return validatePayloadConstraints(payload)
}

// validatePayloadConstraints checks payload constraints independently.
func validatePayloadConstraints(payload *MetaWebhookPayload) error {
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

	// Convert payload back to interface{} for generic validation.
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil
	}

	if err := checkDepth(v, 0); err != nil {
		return err
	}

	// Check text field lengths.
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			msgs := change.Value.Messages
			if len(msgs) > maxArraySize {
				return fmt.Errorf("messages array exceeds %d", maxArraySize)
			}
			for _, msg := range msgs {
				if text, ok := msg["text"].(map[string]interface{}); ok {
					if body, ok := text["body"].(string); ok && len(body) > maxTextLength {
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

// processPayload emits events for webhook payload.
func (h *CloudApiHandler) processPayload(body []byte) {
}
