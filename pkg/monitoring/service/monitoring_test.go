package monitoring_service

import (
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	monitoring_model "github.com/evolution-foundation/evolution-go/pkg/monitoring/model"
)

var t0 = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func ev(instance, kind string, at time.Time) monitoring_model.MetricEvent {
	return monitoring_model.MetricEvent{InstanceID: instance, Kind: kind, CreatedAt: at}
}

var settings = alertSettings{disconnectAfter: 5 * time.Minute, failureThreshold: 3}

func TestShortDisconnectDoesNotAlert(t *testing.T) {
	a := newAlertEngine()
	a.observe(ev("i1", monitoring_model.KindDisconnected, t0), settings)
	if got := a.tick(t0.Add(4*time.Minute), settings); len(got) != 0 {
		t.Fatalf("alerted before the delay: %+v", got)
	}
	if got := a.observe(ev("i1", monitoring_model.KindConnected, t0.Add(4*time.Minute)), settings); len(got) != 0 {
		t.Fatalf("recovery sent for an outage that was never alerted: %+v", got)
	}
	if got := a.tick(t0.Add(time.Hour), settings); len(got) != 0 {
		t.Fatalf("alerted after reconnecting: %+v", got)
	}
}

func TestLongDisconnectAlertsOnceAndRecovers(t *testing.T) {
	a := newAlertEngine()
	a.observe(ev("i1", monitoring_model.KindDisconnected, t0), settings)
	// Failed reconnect attempts keep the start of the outage; the alert shows the latest reason.
	a.observe(ev("i1", monitoring_model.KindConnectFailure, t0.Add(time.Minute)), settings)

	got := a.tick(t0.Add(6*time.Minute), settings)
	if len(got) != 1 || got[0].Type != AlertInstanceDown || !got[0].Since.Equal(t0) || got[0].Reason != monitoring_model.KindConnectFailure {
		t.Fatalf("expected one down alert since t0, got %+v", got)
	}
	if again := a.tick(t0.Add(20*time.Minute), settings); len(again) != 0 {
		t.Fatalf("down alert repeated: %+v", again)
	}
	rec := a.observe(ev("i1", monitoring_model.KindConnected, t0.Add(30*time.Minute)), settings)
	if len(rec) != 1 || rec[0].Type != AlertRecovered {
		t.Fatalf("expected recovery alert, got %+v", rec)
	}
	if msg := alertMessage(rec[0], time.UTC); !strings.Contains(msg, "30 min") {
		t.Fatalf("recovery message without the outage duration: %s", msg)
	}
}

func TestLogoutAlertsImmediately(t *testing.T) {
	a := newAlertEngine()
	got := a.observe(ev("i1", monitoring_model.KindLoggedOut, t0), settings)
	if len(got) != 1 || got[0].Type != AlertLoggedOut {
		t.Fatalf("expected logout alert, got %+v", got)
	}
	if again := a.tick(t0.Add(time.Hour), settings); len(again) != 0 {
		t.Fatalf("down alert after the logout alert: %+v", again)
	}
}

func TestRestartExpectsInstancesBack(t *testing.T) {
	a := newAlertEngine()
	a.markDown([]string{"i1", "i2"}, t0)
	a.observe(ev("i1", monitoring_model.KindConnected, t0.Add(time.Minute)), settings)
	got := a.tick(t0.Add(10*time.Minute), settings)
	if len(got) != 1 || got[0].InstanceID != "i2" || got[0].Reason != monitoring_model.KindServiceStart {
		t.Fatalf("expected only i2 down after restart, got %+v", got)
	}
}

func TestFailureThresholdAndCooldown(t *testing.T) {
	a := newAlertEngine()
	fail := func(at time.Time) []Alert {
		e := ev("i1", monitoring_model.KindFailure, at)
		e.Detail = "webhook"
		return a.observe(e, settings)
	}
	fail(t0)
	fail(t0.Add(time.Minute))
	// Out of the window: the first two no longer count.
	if got := fail(t0.Add(20 * time.Minute)); len(got) != 0 {
		t.Fatalf("alerted with failures outside the window: %+v", got)
	}
	fail(t0.Add(21 * time.Minute))
	got := fail(t0.Add(22 * time.Minute))
	if len(got) != 1 || got[0].Count != 3 || got[0].Reason != "webhook" {
		t.Fatalf("expected failure alert with 3 failures, got %+v", got)
	}
	for i := 0; i < 5; i++ {
		if again := fail(t0.Add(time.Duration(23+i) * time.Minute)); len(again) != 0 {
			t.Fatalf("failure alert repeated within the cooldown: %+v", again)
		}
	}
}

func TestUptime(t *testing.T) {
	from, to := t0, t0.Add(10*time.Hour)
	before := []monitoring_model.MetricEvent{ev("up", monitoring_model.KindConnected, from.Add(-time.Hour))}
	during := []monitoring_model.MetricEvent{
		ev("up", monitoring_model.KindDisconnected, from.Add(2*time.Hour)),
		ev("up", monitoring_model.KindConnected, from.Add(3*time.Hour)),
		ev("late", monitoring_model.KindConnected, from.Add(5*time.Hour)),
		ev("", monitoring_model.KindServiceStart, from.Add(8*time.Hour)),
		ev("up", monitoring_model.KindConnected, from.Add(9*time.Hour)),
	}
	got := uptimeByInstance([]string{"up", "late", "never"}, before, during, from, to)

	// up: online 0-2h, 3-8h, 9-10h = 8h of 10h.
	if got["up"] == nil || *got["up"] != 80 {
		t.Fatalf("up: expected 80%%, got %v", got["up"])
	}
	// late: unknown until 5h, online 5-8h, offline 8-10h = 3h of 5h.
	if got["late"] == nil || *got["late"] != 60 {
		t.Fatalf("late: expected 60%%, got %v", got["late"])
	}
	// never: unknown until the restart at 8h, then offline.
	if got["never"] == nil || *got["never"] != 0 {
		t.Fatalf("never: expected 0%%, got %v", got["never"])
	}
	if none := uptimeByInstance([]string{"x"}, nil, nil, from, to); none["x"] != nil {
		t.Fatalf("expected no uptime without data, got %v", *none["x"])
	}
}

func TestAggregate(t *testing.T) {
	sp, _ := time.LoadLocation("America/Sao_Paulo")
	msg := func(chat string, group bool, at time.Time) monitoring_model.MetricEvent {
		return monitoring_model.MetricEvent{InstanceID: "i1", Kind: monitoring_model.KindMessageIn, Detail: "text", Chat: chat, IsGroup: group, CreatedAt: at}
	}
	late := time.Date(2026, 10, 28, 1, 0, 0, 0, time.UTC) // 22:00 of the 27th in São Paulo
	batch := []monitoring_model.MetricEvent{
		msg("5511", false, late),
		msg("5511", false, late.Add(-time.Hour)),
		msg("group", true, late),
		{InstanceID: "i1", Kind: monitoring_model.KindConnectFailure, Detail: "some reason", CreatedAt: late},
	}
	daily, contacts := aggregate(batch, sp)

	counts := map[string]int64{}
	for _, d := range daily {
		counts[d.Day+"|"+d.Kind+"|"+d.Detail] = d.Count
	}
	if counts["2026-10-27|msg_in|text"] != 3 {
		t.Fatalf("messages not grouped by local day: %v", counts)
	}
	if counts["2026-10-27|connect_failure|"] != 1 {
		t.Fatalf("disconnect reason must not be a daily key: %v", counts)
	}
	if len(contacts) != 1 || contacts[0].Chat != "5511" || !contacts[0].FirstSeen.Equal(late.Add(-time.Hour)) {
		t.Fatalf("expected one contact first seen at the earliest message, got %+v", contacts)
	}
}

func TestMessageType(t *testing.T) {
	cases := map[string]*waE2E.Message{
		"text":     {Conversation: proto.String("oi")},
		"audio":    {AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true)}},
		"image":    {ImageMessage: &waE2E.ImageMessage{}},
		"document": {DocumentWithCaptionMessage: &waE2E.FutureProofMessage{}},
		"":         {ProtocolMessage: &waE2E.ProtocolMessage{}},
		"other":    {},
	}
	for want, m := range cases {
		if got := MessageType(m); got != want {
			t.Errorf("MessageType(%v) = %q, want %q", m, got, want)
		}
	}
	if MessageType(nil) != "" {
		t.Error("nil message must be ignored")
	}
}

func TestSplitNumbers(t *testing.T) {
	got := splitNumbers("+55 (98) 3190-9080, 5511999998888;\n")
	if strings.Join(got, ",") != "559831909080,5511999998888" {
		t.Fatalf("unexpected split: %v", got)
	}
}

func TestEscapeLabel(t *testing.T) {
	if got := escapeLabel("a\"b\\c\nd"); got != `a\"b\\c\nd` {
		t.Fatalf("got %s", got)
	}
}
