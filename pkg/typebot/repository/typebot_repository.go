package typebot_repository

import (
	typebot_model "github.com/evolution-foundation/evolution-go/pkg/typebot/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TypebotRepository interface {
	GetConfig(instanceID string) (*typebot_model.TypebotConfig, error)
	SaveConfig(cfg *typebot_model.TypebotConfig) error
	DeleteConfig(instanceID string) error

	GetSession(instanceID, remoteJid string) (*typebot_model.TypebotSession, error)
	SaveSession(session *typebot_model.TypebotSession) error
	DeleteSession(instanceID, remoteJid string) error
	ListSessions(instanceID string) ([]typebot_model.TypebotSession, error)
	DeleteSessions(instanceID string) error
}

type typebotRepository struct {
	db *gorm.DB
}

func (r *typebotRepository) GetConfig(instanceID string) (*typebot_model.TypebotConfig, error) {
	// Limit/Find instead of First: a miss is the common case and First logs it as an error.
	var cfg typebot_model.TypebotConfig
	res := r.db.Where("instance_id = ?", instanceID).Limit(1).Find(&cfg)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &cfg, nil
}

func (r *typebotRepository) SaveConfig(cfg *typebot_model.TypebotConfig) error {
	// Explicit upsert: Save() with a preset primary key only issues an UPDATE.
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "instance_id"}},
		UpdateAll: true,
	}).Create(cfg).Error
}

func (r *typebotRepository) DeleteConfig(instanceID string) error {
	return r.db.Where("instance_id = ?", instanceID).Delete(&typebot_model.TypebotConfig{}).Error
}

func (r *typebotRepository) GetSession(instanceID, remoteJid string) (*typebot_model.TypebotSession, error) {
	var s typebot_model.TypebotSession
	res := r.db.Where("instance_id = ? AND remote_jid = ?", instanceID, remoteJid).Limit(1).Find(&s)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &s, nil
}

func (r *typebotRepository) SaveSession(session *typebot_model.TypebotSession) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "instance_id"}, {Name: "remote_jid"}},
		DoUpdates: clause.AssignmentColumns([]string{"session_id", "status", "input", "updated_at"}),
	}).Create(session).Error
}

func (r *typebotRepository) DeleteSession(instanceID, remoteJid string) error {
	return r.db.Where("instance_id = ? AND remote_jid = ?", instanceID, remoteJid).Delete(&typebot_model.TypebotSession{}).Error
}

func (r *typebotRepository) ListSessions(instanceID string) ([]typebot_model.TypebotSession, error) {
	var sessions []typebot_model.TypebotSession
	err := r.db.Where("instance_id = ?", instanceID).Order("updated_at DESC").Find(&sessions).Error
	return sessions, err
}

func (r *typebotRepository) DeleteSessions(instanceID string) error {
	return r.db.Where("instance_id = ?", instanceID).Delete(&typebot_model.TypebotSession{}).Error
}

func NewTypebotRepository(db *gorm.DB) TypebotRepository {
	return &typebotRepository{db: db}
}
