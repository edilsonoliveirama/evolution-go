package instance_model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Instance struct {
	Id               string    `json:"id" gorm:"type:uuid;primaryKey"`
	Name             string    `json:"name"`
	Token            string    `json:"token" gorm:"unique"`
	Webhook          string    `json:"webhook"`
	RabbitmqEnable   string    `json:"rabbitmqEnable"`
	WebSocketEnable  string    `json:"websocketEnable"`
	NatsEnable       string    `json:"natsEnable"`
	Jid              string    `json:"jid" gorm:"column:jid"`
	Qrcode           string    `json:"qrcode" gorm:"type:text"`
	Connected        bool      `json:"connected"`
	Expiration       int64     `json:"expiration"`
	DisconnectReason string    `json:"disconnect_reason"`
	Events           string    `json:"events"`
	OsName           string    `json:"os_name"`
	Proxy            string    `json:"proxy"`
	ClientName       string    `json:"client_name"`
	CreatedAt        time.Time `json:"createdAt" gorm:"autoCreateTime"`

	// Engine type: "whatsmeow" (default) | "cloudapi"
	Engine string `json:"engine" gorm:"default:whatsmeow"`

	// Cloud API credentials (engine == "cloudapi")
	MetaWabaId        string `json:"metaWabaId,omitempty" gorm:"column:meta_waba_id"`
	MetaPhoneNumberId string `json:"-" gorm:"column:meta_phone_number_id"`   // sempre mascarado em respostas JSON
	MetaToken         string `json:"-" gorm:"column:meta_token"`             // encrypted at rest, nunca exposto
	MetaAppSecret     string `json:"-" gorm:"column:meta_app_secret"`        // encrypted at rest, nunca exposto
	MetaVerifyToken   string `json:"-" gorm:"column:meta_verify_token"`      // encrypted at rest, nunca exposto
	MetaBusinessName  string `json:"metaBusinessName,omitempty" gorm:"column:meta_business_name"`
	MetaPhoneNumber   string `json:"metaPhoneNumber,omitempty" gorm:"column:meta_phone_number"`

	// Advanced Settings
	AlwaysOnline  bool   `json:"alwaysOnline" gorm:"default:false"`
	RejectCall    bool   `json:"rejectCall" gorm:"default:false"`
	MsgRejectCall string `json:"msgRejectCall" gorm:"default:''"`
	ReadMessages  bool   `json:"readMessages" gorm:"default:false"`
	IgnoreGroups  bool   `json:"ignoreGroups" gorm:"default:false"`
	IgnoreStatus  bool   `json:"ignoreStatus" gorm:"default:false"`
}

// AdvancedSettings representa as configurações avançadas de uma instância
type AdvancedSettings struct {
	AlwaysOnline  bool   `json:"alwaysOnline"`
	RejectCall    bool   `json:"rejectCall"`
	MsgRejectCall string `json:"msgRejectCall"`
	ReadMessages  bool   `json:"readMessages"`
	IgnoreGroups  bool   `json:"ignoreGroups"`
	IgnoreStatus  bool   `json:"ignoreStatus"`
}

func (m *Instance) BeforeCreate(tx *gorm.DB) (err error) {
	if m.Id == "" {
		m.Id = uuid.New().String()
	}
	return
}
