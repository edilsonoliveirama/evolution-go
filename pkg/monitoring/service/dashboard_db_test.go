package monitoring_service

// End to end against a disposable PostgreSQL database. Skipped unless configured:
//
//	METRICS_TEST_DB="host=localhost port=5432 user=postgres password=postgres dbname=metrics_test sslmode=disable"

import (
	"os"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
	instance_repository "github.com/evolution-foundation/evolution-go/pkg/instance/repository"
	monitoring_model "github.com/evolution-foundation/evolution-go/pkg/monitoring/model"
	monitoring_repository "github.com/evolution-foundation/evolution-go/pkg/monitoring/repository"
)

type fakeInstances struct {
	instance_repository.InstanceRepository
	list []*instance_model.Instance
}

func (f fakeInstances) GetAll(string) ([]*instance_model.Instance, error) { return f.list, nil }

func TestDashboardEndToEnd(t *testing.T) {
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

	inst := &instance_model.Instance{Id: "11111111-1111-1111-1111-111111111111", Name: "loja"}
	svc := NewMonitoringService(monitoring_repository.NewMonitoringRepository(db), fakeInstances{list: []*instance_model.Instance{inst}},
		nil, nil, "", "America/Sao_Paulo", 90).(*monitoringService)
	now := time.Now()
	connectedAt := now.Add(-30 * time.Minute)
	if connectedAt.Day() != now.Day() {
		connectedAt = now.Add(-time.Second) // just after midnight: stay within today
	}
	svc.now = func() time.Time { return connectedAt }
	svc.HandleWhatsappEvent(&instance_model.Instance{Id: "11111111-1111-1111-1111-111111111111"}, nil, &events.Connected{})
	svc.now = func() time.Time { return now }

	chat := types.NewJID("5598999990000", types.DefaultUserServer)
	message := func(fromMe bool, m *waE2E.Message, at time.Time) *events.Message {
		return &events.Message{Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, IsFromMe: fromMe},
			Timestamp:     at,
		}, Message: m}
	}
	svc.HandleWhatsappEvent(inst, nil, message(false, &waE2E.Message{Conversation: proto.String("oi")}, now.Add(-time.Minute)))
	svc.HandleWhatsappEvent(inst, nil, message(false, &waE2E.Message{AudioMessage: &waE2E.AudioMessage{}}, now.Add(-time.Minute)))
	svc.HandleWhatsappEvent(inst, nil, message(true, &waE2E.Message{Conversation: proto.String("olá")}, now))
	svc.HandleWhatsappEvent(inst, nil, message(true, &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{}}, now))
	svc.HandleWhatsappEvent(inst, nil, &events.Receipt{MessageSource: types.MessageSource{Chat: chat}, MessageIDs: []string{"a", "b"}, Type: types.ReceiptTypeRead})
	svc.RecordFailure(inst.Id, "webhook")

	var batch []monitoring_model.MetricEvent
	for len(svc.events) > 0 {
		batch = append(batch, <-svc.events)
	}
	svc.flush(batch)

	d, err := svc.Dashboard(1, "")
	if err != nil {
		t.Fatal(err)
	}
	tot := d.Totals
	if tot.MessagesIn != 2 || tot.MessagesOut != 1 || tot.Read != 2 || tot.Failures != 1 || tot.NewContacts != 1 || tot.ActiveChats != 1 {
		t.Fatalf("unexpected totals: %+v", tot)
	}
	if len(d.Daily) != 1 || d.Daily[0].In != 2 || len(d.Hourly) == 0 {
		t.Fatalf("unexpected series: daily=%+v hourly=%d", d.Daily, len(d.Hourly))
	}
	if len(d.Instances) != 1 || d.Instances[0].UptimePercent == nil || *d.Instances[0].UptimePercent != 100 {
		t.Fatalf("unexpected instance health: %+v", d.Instances)
	}
	if len(d.Heatmap) != 1 || d.Heatmap[0].Count != 2 {
		t.Fatalf("unexpected heatmap: %+v", d.Heatmap)
	}

	evs, err := svc.Events("", 10)
	if err != nil || len(evs) != 2 || evs[0].InstanceName != "loja" {
		t.Fatalf("unexpected events: %+v %v", evs, err)
	}

	var prom strings.Builder
	if err := svc.WritePrometheus(&prom); err != nil {
		t.Fatal(err)
	}
	want := `evogo_messages_total{instance_id="11111111-1111-1111-1111-111111111111",instance_name="loja",direction="in",type="audio"} 1`
	if !strings.Contains(prom.String(), want) {
		t.Fatalf("prometheus output missing %q:\n%s", want, prom.String())
	}
}
