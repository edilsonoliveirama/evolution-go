package chatwoot_model

import "time"

// ChatwootConfig holds the per-instance Chatwoot integration settings.
type ChatwootConfig struct {
	InstanceID string `json:"instanceId" gorm:"type:uuid;primaryKey"`
	Enabled    bool   `json:"enabled"`

	// Chatwoot account access
	URL       string `json:"url"`
	AccountID int    `json:"accountId"`
	Token     string `json:"token"`

	// Inbox (API channel) used by this instance
	InboxName       string `json:"inboxName"`
	InboxID         int    `json:"inboxId"`
	InboxIdentifier string `json:"inboxIdentifier"`
	AutoCreateInbox bool   `json:"autoCreateInbox"`

	// Public base URL of this Evolution GO server, reachable by Chatwoot. The
	// inbox webhook is set to <WebhookBaseURL>/chatwoot/webhook/<instanceId>/<WebhookSecret>.
	WebhookBaseURL string `json:"webhookBaseUrl"`
	WebhookSecret  string `json:"-"`

	// Behaviour
	SignMessages        bool   `json:"signMessages"`
	SignDelimiter       string `json:"signDelimiter"`
	ReopenConversation  bool   `json:"reopenConversation"`
	ConversationPending bool   `json:"conversationPending"`
	MergeBrazilContacts bool   `json:"mergeBrazilContacts"`
	IgnoreJids          string `json:"ignoreJids"`

	// Import
	ImportContacts  bool `json:"importContacts"`
	ImportMessages  bool `json:"importMessages"`
	DaysLimitImport int  `json:"daysLimitImport"`

	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

// ChatwootMessage maps a WhatsApp message to the Chatwoot message created for it
// (or the other way around), so replies, deletions and read receipts can be
// correlated in both directions.
type ChatwootMessage struct {
	ID                uint   `gorm:"primaryKey"`
	InstanceID        string `gorm:"type:uuid;uniqueIndex:idx_cw_instance_wa;index:idx_cw_instance_cw"`
	WaMessageID       string `gorm:"uniqueIndex:idx_cw_instance_wa"`
	ChatwootMessageID int    `gorm:"index:idx_cw_instance_cw"`
	ConversationID    int    `gorm:"index"`
	ChatJid           string `gorm:"index"`
	SenderJid         string
	FromMe            bool
	CreatedAt         time.Time `gorm:"autoCreateTime"`
}
