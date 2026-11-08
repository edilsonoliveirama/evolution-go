package cloudapi

import (
	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
	"time"
)

// Instance is an alias for instance_model.Instance, used throughout the
// cloudapi package so tests and code can refer to the canonical Instance type.
type Instance = instance_model.Instance

// CloudApiConfig holds configuration for the Cloud API engine.
type CloudApiConfig struct {
	GraphApiVersion       string
	HttpTimeoutSeconds    int
	WebhookMaxBodySizeMB  int
	HealthCheckIntervalMin int
	MediaUrlAllowlist     []string
	EncryptionKey         []byte
	EncryptionKeyPrev     []byte
}

// CloudStatus represents the current connection status of a Cloud API instance.
type CloudStatus struct {
	Connected        bool   `json:"connected"`
	PhoneNumber      string `json:"phoneNumber,omitempty"`
	BusinessName     string `json:"businessName,omitempty"`
	DisconnectReason string `json:"disconnectReason,omitempty"`
	LastValidatedAt  *time.Time `json:"lastValidatedAt,omitempty"`
}

// SendResponse represents the response from the Meta Graph API after sending a message.
type SendResponse struct {
	MessagingProduct string              `json:"messaging_product"`
	Contacts         []SendContact       `json:"contacts,omitempty"`
	Messages         []SendMessageResult `json:"messages,omitempty"`
}

// SendContact represents a contact in the send response.
type SendContact struct {
	Input string `json:"input"`
	WaID  string `json:"wa_id"`
}

// SendMessageResult represents a message result in the send response.
type SendMessageResult struct {
	ID string `json:"id"`
}

// BusinessProfile represents a WhatsApp Business Profile from the Graph API.
type BusinessProfile struct {
	Data []BusinessProfileData `json:"data"`
}

// BusinessProfileData contains the business profile data.
type BusinessProfileData struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// PhoneNumberInfo represents phone number information from the Graph API.
type PhoneNumberInfo struct {
	VerifiedName           string `json:"verified_name"`
	DisplayPhoneNumber     string `json:"display_phone_number"`
	ID                     string `json:"id"`
	QualityRating          string `json:"quality_rating,omitempty"`
	CodeVerificationStatus string `json:"code_verification_status,omitempty"`
}

// MediaUploadResponse represents the response from the Meta Media API upload.
type MediaUploadResponse struct {
	ID string `json:"id"`
}

// MetaWebhookPayload represents the top-level webhook payload from Meta.
type MetaWebhookPayload struct {
	Object string             `json:"object"`
	Entry  []MetaWebhookEntry `json:"entry"`
}

// MetaWebhookEntry represents an entry in the webhook payload.
type MetaWebhookEntry struct {
	ID      string              `json:"id"`
	Changes []MetaWebhookChange `json:"changes"`
}

// MetaWebhookChange represents a change in the webhook entry.
type MetaWebhookChange struct {
	Value MetaWebhookValue `json:"value"`
	Field string           `json:"field"`
}

// MetaWebhookValue represents the value of a webhook change.
type MetaWebhookValue struct {
	MessagingProduct string              `json:"messaging_product"`
	Metadata         MetaWebhookMetadata `json:"metadata"`
	Contacts         []map[string]interface{} `json:"contacts,omitempty"`
	Messages         []map[string]interface{} `json:"messages,omitempty"`
	Statuses         []map[string]interface{} `json:"statuses,omitempty"`
}

// MetaWebhookMetadata represents metadata in a webhook value.
type MetaWebhookMetadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

// TemplateMessage represents a WhatsApp template message to send.
type TemplateMessage struct {
	Name       string              `json:"name"`
	Language   string              `json:"language"`
	Components []TemplateComponent `json:"components"`
}

// TemplateComponent represents a component of a template message.
type TemplateComponent struct {
	Type       string               `json:"type"`
	Parameters []TemplateParameter  `json:"parameters"`
}

// TemplateParameter represents a parameter of a template component.
type TemplateParameter struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// URLValidator defines the interface for URL validation (SSRF protection).
type URLValidator interface {
	ValidateURL(rawURL string) error
}
