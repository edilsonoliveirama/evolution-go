package monitoring_repository

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	monitoring_model "github.com/evolution-foundation/evolution-go/pkg/monitoring/model"
)

// MonitoringRepository stores the monitoring events. The queries use
// PostgreSQL syntax (the users database is always PostgreSQL). An empty
// instanceID means every instance.
type MonitoringRepository interface {
	Write(events []monitoring_model.MetricEvent, daily []monitoring_model.MetricDaily, contacts []monitoring_model.MetricContact) error
	PruneEvents(before time.Time) (int64, error)

	Daily(fromDay, toDay, instanceID string) ([]monitoring_model.MetricDaily, error)
	NewContactsPerDay(from, to time.Time, instanceID, tz string) ([]monitoring_model.DayCount, error)
	ActiveChatsPerDay(from, to time.Time, instanceID, tz string) ([]monitoring_model.DayCount, error)
	ActiveChats(from, to time.Time, instanceID string) (int64, error)
	HourlyMessages(from, to time.Time, instanceID, tz string) ([]monitoring_model.BucketCount, error)
	Heatmap(from, to time.Time, instanceID, tz string) ([]monitoring_model.HeatCell, error)

	// StateEvents returns the connection state events in [from, to), oldest first.
	StateEvents(from, to time.Time) ([]monitoring_model.MetricEvent, error)
	// LastStatesBefore returns the last state event of each instance (and the
	// last service start) before t.
	LastStatesBefore(t time.Time) ([]monitoring_model.MetricEvent, error)
	RecentEvents(instanceID string, kinds []string, limit int) ([]monitoring_model.MetricEvent, error)

	GetAlertConfig() (*monitoring_model.AlertConfig, error)
	SaveAlertConfig(cfg *monitoring_model.AlertConfig) error
}

type monitoringRepository struct {
	db *gorm.DB
}

var stateKinds = []string{
	monitoring_model.KindConnected, monitoring_model.KindDisconnected, monitoring_model.KindLoggedOut,
	monitoring_model.KindConnectFailure, monitoring_model.KindTemporaryBan, monitoring_model.KindStreamReplaced,
	monitoring_model.KindServiceStart,
}

var messageKinds = []string{monitoring_model.KindMessageIn, monitoring_model.KindMessageOut}

func scoped(q *gorm.DB, instanceID string) *gorm.DB {
	if instanceID != "" {
		return q.Where("instance_id = ?", instanceID)
	}
	return q
}

func (r *monitoringRepository) Write(events []monitoring_model.MetricEvent, daily []monitoring_model.MetricDaily, contacts []monitoring_model.MetricContact) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if len(events) > 0 {
			if err := tx.CreateInBatches(events, 500).Error; err != nil {
				return err
			}
		}
		if len(daily) > 0 {
			// daily holds one row per key, as one statement can't update a row twice.
			err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "day"}, {Name: "instance_id"}, {Name: "kind"}, {Name: "detail"}},
				DoUpdates: clause.Assignments(map[string]interface{}{"count": gorm.Expr("metric_daily.count + excluded.count")}),
			}).Create(&daily).Error
			if err != nil {
				return err
			}
		}
		if len(contacts) > 0 {
			// Keep the first FirstSeen.
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&contacts).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *monitoringRepository) PruneEvents(before time.Time) (int64, error) {
	res := r.db.Where("created_at < ?", before).Delete(&monitoring_model.MetricEvent{})
	return res.RowsAffected, res.Error
}

func (r *monitoringRepository) Daily(fromDay, toDay, instanceID string) ([]monitoring_model.MetricDaily, error) {
	var rows []monitoring_model.MetricDaily
	q := scoped(r.db.Where("day >= ? AND day <= ?", fromDay, toDay), instanceID)
	err := q.Order("day").Find(&rows).Error
	return rows, err
}

func (r *monitoringRepository) NewContactsPerDay(from, to time.Time, instanceID, tz string) ([]monitoring_model.DayCount, error) {
	var rows []monitoring_model.DayCount
	q := r.db.Model(&monitoring_model.MetricContact{}).
		Select("to_char(first_seen AT TIME ZONE ?, 'YYYY-MM-DD') AS day, count(*) AS count", tz).
		Where("first_seen >= ? AND first_seen < ?", from, to)
	err := scoped(q, instanceID).Group("day").Order("day").Scan(&rows).Error
	return rows, err
}

func (r *monitoringRepository) ActiveChatsPerDay(from, to time.Time, instanceID, tz string) ([]monitoring_model.DayCount, error) {
	var rows []monitoring_model.DayCount
	q := r.db.Model(&monitoring_model.MetricEvent{}).
		Select("to_char(created_at AT TIME ZONE ?, 'YYYY-MM-DD') AS day, count(DISTINCT instance_id || '|' || chat) AS count", tz).
		Where("kind IN ? AND created_at >= ? AND created_at < ? AND NOT is_group", messageKinds, from, to)
	err := scoped(q, instanceID).Group("day").Order("day").Scan(&rows).Error
	return rows, err
}

func (r *monitoringRepository) ActiveChats(from, to time.Time, instanceID string) (int64, error) {
	var n int64
	q := r.db.Model(&monitoring_model.MetricEvent{}).
		Select("count(DISTINCT instance_id || '|' || chat)").
		Where("kind IN ? AND created_at >= ? AND created_at < ? AND NOT is_group", messageKinds, from, to)
	err := scoped(q, instanceID).Scan(&n).Error
	return n, err
}

func (r *monitoringRepository) HourlyMessages(from, to time.Time, instanceID, tz string) ([]monitoring_model.BucketCount, error) {
	var rows []monitoring_model.BucketCount
	q := r.db.Model(&monitoring_model.MetricEvent{}).
		Select(`to_char(created_at AT TIME ZONE ?, 'YYYY-MM-DD"T"HH24:00') AS bucket, kind, count(*) AS count`, tz).
		Where("kind IN ? AND created_at >= ? AND created_at < ?", messageKinds, from, to)
	err := scoped(q, instanceID).Group("bucket, kind").Order("bucket").Scan(&rows).Error
	return rows, err
}

func (r *monitoringRepository) Heatmap(from, to time.Time, instanceID, tz string) ([]monitoring_model.HeatCell, error) {
	var rows []monitoring_model.HeatCell
	q := r.db.Model(&monitoring_model.MetricEvent{}).
		Select(`extract(isodow FROM created_at AT TIME ZONE ?)::int AS dow,
			extract(hour FROM created_at AT TIME ZONE ?)::int AS hour, count(*) AS count`, tz, tz).
		Where("kind = ? AND created_at >= ? AND created_at < ?", monitoring_model.KindMessageIn, from, to)
	err := scoped(q, instanceID).Group("dow, hour").Scan(&rows).Error
	return rows, err
}

func (r *monitoringRepository) StateEvents(from, to time.Time) ([]monitoring_model.MetricEvent, error) {
	var rows []monitoring_model.MetricEvent
	err := r.db.Where("kind IN ? AND created_at >= ? AND created_at < ?", stateKinds, from, to).
		Order("created_at, id").Find(&rows).Error
	return rows, err
}

func (r *monitoringRepository) LastStatesBefore(t time.Time) ([]monitoring_model.MetricEvent, error) {
	var rows []monitoring_model.MetricEvent
	err := r.db.Raw(`SELECT DISTINCT ON (instance_id) * FROM metric_events
		WHERE kind IN ? AND created_at < ?
		ORDER BY instance_id, created_at DESC, id DESC`, stateKinds, t).Scan(&rows).Error
	return rows, err
}

func (r *monitoringRepository) RecentEvents(instanceID string, kinds []string, limit int) ([]monitoring_model.MetricEvent, error) {
	var rows []monitoring_model.MetricEvent
	q := scoped(r.db.Where("kind IN ?", kinds), instanceID)
	err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *monitoringRepository) GetAlertConfig() (*monitoring_model.AlertConfig, error) {
	// Limit/Find instead of First: a miss is the common case and First logs it as an error.
	var cfg monitoring_model.AlertConfig
	res := r.db.Where("id = ?", 1).Limit(1).Find(&cfg)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &cfg, nil
}

func (r *monitoringRepository) SaveAlertConfig(cfg *monitoring_model.AlertConfig) error {
	cfg.ID = 1
	// Explicit upsert: Save() with a preset primary key only issues an UPDATE.
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(cfg).Error
}

func NewMonitoringRepository(db *gorm.DB) MonitoringRepository {
	return &monitoringRepository{db: db}
}
