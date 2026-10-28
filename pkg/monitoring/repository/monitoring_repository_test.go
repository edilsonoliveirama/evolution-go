package monitoring_repository

// Runs against a disposable PostgreSQL database. Skipped unless configured:
//
//	METRICS_TEST_DB="host=localhost port=5432 user=postgres password=postgres dbname=metrics_test sslmode=disable"

import (
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	monitoring_model "github.com/evolution-foundation/evolution-go/pkg/monitoring/model"
)

const tz = "America/Sao_Paulo"

func testRepo(t *testing.T) MonitoringRepository {
	dsn := os.Getenv("METRICS_TEST_DB")
	if dsn == "" {
		t.Skip("METRICS_TEST_DB not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	models := []interface{}{&monitoring_model.MetricEvent{}, &monitoring_model.MetricDaily{}, &monitoring_model.MetricContact{}, &monitoring_model.AlertConfig{}}
	if err := db.Migrator().DropTable(models...); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	return NewMonitoringRepository(db)
}

func TestRepository(t *testing.T) {
	r := testRepo(t)
	// 22:30 of Oct 27 in São Paulo = 01:30 UTC of the 28th.
	base := time.Date(2026, 10, 28, 1, 30, 0, 0, time.UTC)
	msg := func(instance, kind, chat string, group bool, at time.Time) monitoring_model.MetricEvent {
		return monitoring_model.MetricEvent{InstanceID: instance, Kind: kind, Detail: "text", Chat: chat, IsGroup: group, CreatedAt: at}
	}
	events := []monitoring_model.MetricEvent{
		msg("i1", monitoring_model.KindMessageIn, "a", false, base),
		msg("i1", monitoring_model.KindMessageOut, "a", false, base.Add(time.Minute)),
		msg("i1", monitoring_model.KindMessageIn, "b", false, base.Add(2*time.Hour)), // Oct 28 local
		msg("i1", monitoring_model.KindMessageIn, "g", true, base),
		msg("i2", monitoring_model.KindMessageIn, "a", false, base),
		{InstanceID: "i1", Kind: monitoring_model.KindDisconnected, CreatedAt: base.Add(-48 * time.Hour)},
		{InstanceID: "i1", Kind: monitoring_model.KindConnected, CreatedAt: base.Add(-47 * time.Hour)},
		{Kind: monitoring_model.KindServiceStart, CreatedAt: base.Add(-24 * time.Hour)},
		{InstanceID: "i1", Kind: monitoring_model.KindConnected, CreatedAt: base.Add(-23 * time.Hour)},
	}
	daily := []monitoring_model.MetricDaily{{Day: "2026-10-27", InstanceID: "i1", Kind: monitoring_model.KindMessageIn, Detail: "text", Count: 2}}
	contacts := []monitoring_model.MetricContact{
		{InstanceID: "i1", Chat: "a", FirstSeen: base},
		{InstanceID: "i1", Chat: "b", FirstSeen: base.Add(2 * time.Hour)},
	}
	if err := r.Write(events, daily, contacts); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Second batch: counts add up, the first contact time is kept.
	later := []monitoring_model.MetricContact{{InstanceID: "i1", Chat: "a", FirstSeen: base.Add(5 * time.Hour)}}
	if err := r.Write(nil, daily, later); err != nil {
		t.Fatalf("second write: %v", err)
	}

	rows, err := r.Daily("2026-10-27", "2026-10-28", "i1")
	if err != nil || len(rows) != 1 || rows[0].Count != 4 {
		t.Fatalf("daily upsert should add counts: %+v %v", rows, err)
	}

	from, to := base.Add(-72*time.Hour), base.Add(24*time.Hour)
	nc, err := r.NewContactsPerDay(from, to, "i1", tz)
	if err != nil || len(nc) != 2 || nc[0] != (monitoring_model.DayCount{Day: "2026-10-27", Count: 1}) {
		t.Fatalf("new contacts per local day: %+v %v", nc, err)
	}
	active, err := r.ActiveChatsPerDay(from, to, "", tz)
	if err != nil || len(active) != 2 || active[0].Count != 2 {
		t.Fatalf("active chats (groups excluded, per instance): %+v %v", active, err)
	}
	if n, err := r.ActiveChats(from, to, "i1"); err != nil || n != 2 {
		t.Fatalf("active chats of i1: %d %v", n, err)
	}
	hourly, err := r.HourlyMessages(from, to, "i1", tz)
	if err != nil || len(hourly) == 0 || hourly[0].Bucket != "2026-10-27T22:00" {
		t.Fatalf("hourly buckets in local time: %+v %v", hourly, err)
	}
	heat, err := r.Heatmap(from, to, "i1", tz)
	if err != nil {
		t.Fatal(err)
	}
	cells := map[[2]int]int64{}
	for _, c := range heat {
		cells[[2]int{c.Dow, c.Hour}] = c.Count
	}
	// Tuesday 22h: chat a + group; Wednesday 0h: chat b.
	if cells[[2]int{2, 22}] != 2 || cells[[2]int{3, 0}] != 1 {
		t.Fatalf("heatmap: %+v", heat)
	}

	last, err := r.LastStatesBefore(base.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	byInstance := map[string]string{}
	for _, e := range last {
		byInstance[e.InstanceID] = e.Kind
	}
	if byInstance["i1"] != monitoring_model.KindConnected || byInstance[""] != monitoring_model.KindServiceStart {
		t.Fatalf("last states: %+v", last)
	}
	states, err := r.StateEvents(base.Add(-30*time.Hour), base)
	if err != nil || len(states) != 2 || states[0].Kind != monitoring_model.KindServiceStart {
		t.Fatalf("state events oldest first: %+v %v", states, err)
	}

	if n, err := r.PruneEvents(base.Add(-time.Hour)); err != nil || n != 4 {
		t.Fatalf("prune: %d %v", n, err)
	}

	if cfg, err := r.GetAlertConfig(); err != nil || cfg != nil {
		t.Fatalf("no alert config yet: %+v %v", cfg, err)
	}
	cfg := &monitoring_model.AlertConfig{Enabled: true, Numbers: "5598", DisconnectMinutes: 5}
	if err := r.SaveAlertConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = false
	if err := r.SaveAlertConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got, err := r.GetAlertConfig(); err != nil || got == nil || got.Enabled || got.Numbers != "5598" {
		t.Fatalf("alert config upsert: %+v %v", got, err)
	}
}
