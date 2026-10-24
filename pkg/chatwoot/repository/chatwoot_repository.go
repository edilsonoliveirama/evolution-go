package chatwoot_repository

import (
	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ChatwootRepository interface {
	GetConfig(instanceID string) (*chatwoot_model.ChatwootConfig, error)
	GetEnabledConfigs() ([]chatwoot_model.ChatwootConfig, error)
	SaveConfig(cfg *chatwoot_model.ChatwootConfig) error
	DeleteConfig(instanceID string) error

	SaveMessage(msg *chatwoot_model.ChatwootMessage) error
	GetMessageByWaID(instanceID, waMessageID string) (*chatwoot_model.ChatwootMessage, error)
	GetMessageByChatwootID(instanceID string, chatwootMessageID int) (*chatwoot_model.ChatwootMessage, error)
	GetMessagesByChatwootID(instanceID string, chatwootMessageID int) ([]chatwoot_model.ChatwootMessage, error)
	DeleteMessagesByInstance(instanceID string) error
}

type chatwootRepository struct {
	db *gorm.DB
}

func (r *chatwootRepository) GetConfig(instanceID string) (*chatwoot_model.ChatwootConfig, error) {
	// Limit/Find instead of First: a miss is the common case and First logs it as an error.
	var cfg chatwoot_model.ChatwootConfig
	res := r.db.Where("instance_id = ?", instanceID).Limit(1).Find(&cfg)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &cfg, nil
}

func (r *chatwootRepository) GetEnabledConfigs() ([]chatwoot_model.ChatwootConfig, error) {
	var cfgs []chatwoot_model.ChatwootConfig
	err := r.db.Where("enabled = ?", true).Find(&cfgs).Error
	return cfgs, err
}

func (r *chatwootRepository) SaveConfig(cfg *chatwoot_model.ChatwootConfig) error {
	// Explicit upsert: Save() with a preset primary key only issues an UPDATE.
	// The model has no column defaults, so false/zero values are written too.
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "instance_id"}},
		UpdateAll: true,
	}).Create(cfg).Error
}

func (r *chatwootRepository) DeleteConfig(instanceID string) error {
	return r.db.Where("instance_id = ?", instanceID).Delete(&chatwoot_model.ChatwootConfig{}).Error
}

func (r *chatwootRepository) SaveMessage(msg *chatwoot_model.ChatwootMessage) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "instance_id"}, {Name: "wa_message_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"chatwoot_message_id", "conversation_id"}),
	}).Create(msg).Error
}

func (r *chatwootRepository) GetMessageByWaID(instanceID, waMessageID string) (*chatwoot_model.ChatwootMessage, error) {
	var msg chatwoot_model.ChatwootMessage
	res := r.db.Where("instance_id = ? AND wa_message_id = ?", instanceID, waMessageID).Limit(1).Find(&msg)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &msg, nil
}

func (r *chatwootRepository) GetMessageByChatwootID(instanceID string, chatwootMessageID int) (*chatwoot_model.ChatwootMessage, error) {
	var msg chatwoot_model.ChatwootMessage
	res := r.db.Where("instance_id = ? AND chatwoot_message_id = ?", instanceID, chatwootMessageID).Limit(1).Find(&msg)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &msg, nil
}

func (r *chatwootRepository) GetMessagesByChatwootID(instanceID string, chatwootMessageID int) ([]chatwoot_model.ChatwootMessage, error) {
	var msgs []chatwoot_model.ChatwootMessage
	err := r.db.Where("instance_id = ? AND chatwoot_message_id = ?", instanceID, chatwootMessageID).Find(&msgs).Error
	return msgs, err
}

func (r *chatwootRepository) DeleteMessagesByInstance(instanceID string) error {
	return r.db.Where("instance_id = ?", instanceID).Delete(&chatwoot_model.ChatwootMessage{}).Error
}

func NewChatwootRepository(db *gorm.DB) ChatwootRepository {
	return &chatwootRepository{db: db}
}
