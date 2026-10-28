package monitoring_model

import "time"

// Event kinds.
const (
	KindMessageIn  = "msg_in"
	KindMessageOut = "msg_out"
	KindDelivered  = "delivered" // delivery receipt of a message we sent
	KindRead       = "read"      // read receipt of a message we sent
	KindFailure    = "failure"   // Detail = component (webhook, chatwoot, ...)

	KindConnected      = "connected"
	KindDisconnected   = "disconnected"
	KindLoggedOut      = "logged_out"
	KindConnectFailure = "connect_failure"
	KindTemporaryBan   = "temp_ban"
	KindStreamReplaced = "stream_replaced"
	KindPaired         = "paired"
	// Recorded once per process start with an empty InstanceID: every instance
	// is offline until it connects again.
	KindServiceStart = "service_start"
)

// IsStateKind reports whether the kind changes the connection state.
func IsStateKind(kind string) bool {
	switch kind {
	case KindConnected, KindDisconnected, KindLoggedOut, KindConnectFailure,
		KindTemporaryBan, KindStreamReplaced, KindServiceStart:
		return true
	}
	return false
}

// MetricEvent is one recorded occurrence. Kept for METRICS_RETENTION_DAYS;
// MetricDaily keeps the totals after that.
type MetricEvent struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	InstanceID string    `json:"instanceId" gorm:"size:64;index:idx_metric_events_instance_time,priority:1"`
	Kind       string    `json:"kind" gorm:"size:32;index:idx_metric_events_kind_time,priority:1"`
	Detail     string    `json:"detail" gorm:"size:64"` // message type, failure component or disconnect reason
	Chat       string    `json:"-" gorm:"size:64"`
	IsGroup    bool      `json:"-"`
	CreatedAt  time.Time `json:"createdAt" gorm:"index:idx_metric_events_instance_time,priority:2;index:idx_metric_events_kind_time,priority:2;index:idx_metric_events_created_at"`
}

// MetricDaily is the number of events per day, kept forever.
type MetricDaily struct {
	Day        string `json:"day" gorm:"primaryKey;size:10"` // YYYY-MM-DD in METRICS_TIMEZONE
	InstanceID string `json:"instanceId" gorm:"primaryKey;size:64"`
	Kind       string `json:"kind" gorm:"primaryKey;size:32"`
	Detail     string `json:"detail" gorm:"primaryKey;size:64"`
	Count      int64  `json:"count"`
}

func (MetricDaily) TableName() string { return "metric_daily" }

// MetricContact is the first time an individual chat exchanged a message
// with the instance (after monitoring was enabled).
type MetricContact struct {
	InstanceID string    `gorm:"primaryKey;size:64"`
	Chat       string    `gorm:"primaryKey;size:64"`
	FirstSeen  time.Time `gorm:"index"`
}

// AlertConfig is the single row (ID 1) with the alert settings.
type AlertConfig struct {
	ID                 uint      `json:"-" gorm:"primaryKey"`
	Enabled            bool      `json:"enabled"`
	NotifierInstanceID string    `json:"notifierInstanceId" gorm:"size:64"`
	Numbers            string    `json:"numbers"` // comma separated
	WebhookURL         string    `json:"webhookUrl"`
	DisconnectMinutes  int       `json:"disconnectMinutes"`
	FailureThreshold   int       `json:"failureThreshold"` // failures of one component in FailureWindow
	UpdatedAt          time.Time `json:"updatedAt"`
}

func (AlertConfig) TableName() string { return "metric_alert_config" }

// DayCount is a value per day (YYYY-MM-DD).
type DayCount struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

// BucketCount is a value per hour bucket (YYYY-MM-DDTHH:00 in METRICS_TIMEZONE).
type BucketCount struct {
	Bucket string `json:"bucket"`
	Kind   string `json:"kind"`
	Count  int64  `json:"count"`
}

// HeatCell is the number of received messages on a weekday (1 = Monday) and hour.
type HeatCell struct {
	Dow   int   `json:"dow"`
	Hour  int   `json:"hour"`
	Count int64 `json:"count"`
}
