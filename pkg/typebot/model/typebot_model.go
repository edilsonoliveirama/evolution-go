package typebot_model

import "time"

const (
	TriggerAll     = "all"
	TriggerKeyword = "keyword"
	TriggerNone    = "none"

	OperatorEquals     = "equals"
	OperatorContains   = "contains"
	OperatorStartsWith = "startsWith"

	SessionOpened = "opened"
	SessionPaused = "paused"
)

// TypebotConfig holds the per-instance Typebot integration settings.
type TypebotConfig struct {
	InstanceID string `json:"instanceId" gorm:"type:uuid;primaryKey"`
	Enabled    bool   `json:"enabled"`

	// Viewer URL (where published bots are served) and the bot public ID.
	URL      string `json:"url"`
	PublicID string `json:"publicId"`

	// When a contact without an active session starts the bot.
	TriggerType     string `json:"triggerType"`
	TriggerKeywords string `json:"triggerKeywords"`
	TriggerOperator string `json:"triggerOperator"`

	ExpireMinutes  int    `json:"expireMinutes"`
	KeywordFinish  string `json:"keywordFinish"`
	DelayMessageMs int    `json:"delayMessageMs"`
	UnknownMessage string `json:"unknownMessage"`
	// A message sent by a human (phone, Chatwoot, API) pauses the bot in that chat.
	StopBotFromMe bool `json:"stopBotFromMe"`
	// When the flow ends, move the Chatwoot conversation to the agents queue.
	HandoffChatwoot bool   `json:"handoffChatwoot"`
	IgnoreJids      string `json:"ignoreJids"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

// TypebotSession is an active bot conversation with one contact. Rows are
// deleted when the flow ends, expires or is closed.
type TypebotSession struct {
	ID         uint      `json:"-" gorm:"primaryKey"`
	InstanceID string    `json:"instanceId" gorm:"type:uuid;uniqueIndex:idx_tb_instance_jid"`
	RemoteJid  string    `json:"remoteJid" gorm:"uniqueIndex:idx_tb_instance_jid"`
	SessionID  string    `json:"sessionId"`
	Status     string    `json:"status"`
	Input      string    `json:"-" gorm:"type:text"` // JSON of the input the bot is waiting for
	CreatedAt  time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
