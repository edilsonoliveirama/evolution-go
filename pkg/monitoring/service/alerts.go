package monitoring_service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gomessguii/logger"

	monitoring_model "github.com/evolution-foundation/evolution-go/pkg/monitoring/model"
	send_service "github.com/evolution-foundation/evolution-go/pkg/sendMessage/service"
)

const (
	FailureWindow            = 15 * time.Minute
	failureCooldown          = time.Hour
	tickEvery                = 30 * time.Second
	DefaultDisconnectMinutes = 5
	DefaultFailureThreshold  = 10
)

// Alert types.
const (
	AlertInstanceDown = "instance_down"
	AlertLoggedOut    = "instance_logged_out"
	AlertBanned       = "instance_banned"
	AlertRecovered    = "instance_recovered"
	AlertFailures     = "delivery_failures"
	AlertTest         = "test"
)

// Alert is sent to the WhatsApp numbers and posted to the alert webhook.
type Alert struct {
	Type         string    `json:"type"`
	InstanceID   string    `json:"instanceId,omitempty"`
	InstanceName string    `json:"instanceName,omitempty"`
	Reason       string    `json:"reason,omitempty"` // event kind that took the instance down, or the failure component
	Detail       string    `json:"detail,omitempty"`
	Since        time.Time `json:"since,omitzero"`
	Count        int       `json:"count,omitempty"`
	At           time.Time `json:"at"`
	Message      string    `json:"message"`
}

type alertSettings struct {
	disconnectAfter  time.Duration
	failureThreshold int
}

type downState struct {
	since   time.Time
	kind    string
	detail  string
	alerted bool
}

// alertEngine turns the event stream into alerts. It only keeps state;
// delivery is done by the service.
type alertEngine struct {
	mu               sync.Mutex
	down             map[string]*downState
	failures         map[string][]time.Time // instance|component
	lastFailureAlert map[string]time.Time
}

func newAlertEngine() *alertEngine {
	return &alertEngine{
		down:             map[string]*downState{},
		failures:         map[string][]time.Time{},
		lastFailureAlert: map[string]time.Time{},
	}
}

// markDown starts the offline timer of instances that should come back on
// their own (after a restart of the service).
func (a *alertEngine) markDown(instanceIDs []string, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range instanceIDs {
		if _, ok := a.down[id]; !ok {
			a.down[id] = &downState{since: at, kind: monitoring_model.KindServiceStart}
		}
	}
}

func (a *alertEngine) forget(instanceID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.down, instanceID)
}

func (a *alertEngine) observe(ev monitoring_model.MetricEvent, cfg alertSettings) []Alert {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := ev.InstanceID

	switch ev.Kind {
	case monitoring_model.KindConnected:
		st, ok := a.down[id]
		if !ok {
			return nil
		}
		delete(a.down, id)
		if !st.alerted {
			return nil
		}
		return []Alert{{Type: AlertRecovered, InstanceID: id, Reason: st.kind, Since: st.since, At: ev.CreatedAt}}

	case monitoring_model.KindLoggedOut, monitoring_model.KindTemporaryBan:
		st := a.downState(id, ev)
		st.kind, st.detail, st.alerted = ev.Kind, ev.Detail, true
		typ := AlertLoggedOut
		if ev.Kind == monitoring_model.KindTemporaryBan {
			typ = AlertBanned
		}
		// Needs someone to act: no waiting period.
		return []Alert{{Type: typ, InstanceID: id, Reason: ev.Kind, Detail: ev.Detail, Since: st.since, At: ev.CreatedAt}}

	case monitoring_model.KindDisconnected, monitoring_model.KindConnectFailure, monitoring_model.KindStreamReplaced:
		st := a.downState(id, ev)
		if !st.alerted {
			st.kind, st.detail = ev.Kind, ev.Detail
		}
		return nil

	case monitoring_model.KindFailure:
		if cfg.failureThreshold <= 0 {
			return nil
		}
		key := id + "|" + ev.Detail
		recent := a.failures[key][:0]
		for _, t := range a.failures[key] {
			if ev.CreatedAt.Sub(t) < FailureWindow {
				recent = append(recent, t)
			}
		}
		recent = append(recent, ev.CreatedAt)
		a.failures[key] = recent
		if len(recent) < cfg.failureThreshold || ev.CreatedAt.Sub(a.lastFailureAlert[key]) < failureCooldown {
			return nil
		}
		a.lastFailureAlert[key] = ev.CreatedAt
		return []Alert{{Type: AlertFailures, InstanceID: id, Reason: ev.Detail, Count: len(recent), At: ev.CreatedAt}}
	}
	return nil
}

func (a *alertEngine) downState(id string, ev monitoring_model.MetricEvent) *downState {
	st, ok := a.down[id]
	if !ok {
		st = &downState{since: ev.CreatedAt}
		a.down[id] = st
	}
	return st
}

// tick returns the instances offline for longer than the configured delay.
func (a *alertEngine) tick(now time.Time, cfg alertSettings) []Alert {
	a.mu.Lock()
	defer a.mu.Unlock()
	var alerts []Alert
	for id, st := range a.down {
		if st.alerted || now.Sub(st.since) < cfg.disconnectAfter {
			continue
		}
		st.alerted = true
		alerts = append(alerts, Alert{Type: AlertInstanceDown, InstanceID: id, Reason: st.kind, Detail: st.detail, Since: st.since, At: now})
	}
	return alerts
}

// downSince is the start of the current outage of each offline instance.
func (a *alertEngine) downSince() map[string]time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]time.Time, len(a.down))
	for id, st := range a.down {
		out[id] = st.since
	}
	return out
}

func (s *monitoringService) alertSettings() alertSettings {
	cfg := s.alertConfig()
	return alertSettings{
		disconnectAfter:  time.Duration(cfg.DisconnectMinutes) * time.Minute,
		failureThreshold: cfg.FailureThreshold,
	}
}

func (s *monitoringService) alertConfig() *monitoring_model.AlertConfig {
	if cfg := s.alertCfg.Load(); cfg != nil {
		return cfg
	}
	return defaultAlertConfig()
}

func defaultAlertConfig() *monitoring_model.AlertConfig {
	return &monitoring_model.AlertConfig{
		DisconnectMinutes: DefaultDisconnectMinutes,
		FailureThreshold:  DefaultFailureThreshold,
	}
}

func (s *monitoringService) tickLoop() {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for range ticker.C {
		if alerts := s.alerts.tick(s.now(), s.alertSettings()); len(alerts) > 0 {
			s.deliver(alerts)
		}
	}
}

func (s *monitoringService) deliver(alerts []Alert) {
	cfg := s.alertConfig()
	if !cfg.Enabled {
		return
	}
	for _, a := range alerts {
		inst, err := s.instanceRepository.GetInstanceByID(a.InstanceID)
		if err != nil || inst == nil {
			// Deleted instance.
			s.alerts.forget(a.InstanceID)
			continue
		}
		if a.Type == AlertInstanceDown && s.isOnline(a.InstanceID) {
			// Reconnected without a Connected event reaching us.
			s.alerts.forget(a.InstanceID)
			continue
		}
		a.InstanceName = inst.Name
		a.Message = alertMessage(a, s.loc)
		if err := s.send(cfg, a); err != nil {
			logger.LogWarn("[monitoring] alert %s for %s not fully delivered: %v", a.Type, a.InstanceName, err)
		}
	}
}

// send delivers one alert to every configured channel.
func (s *monitoringService) send(cfg *monitoring_model.AlertConfig, a Alert) error {
	var errs []error
	numbers := splitNumbers(cfg.Numbers)
	if cfg.NotifierInstanceID != "" && len(numbers) > 0 {
		notifier, err := s.instanceRepository.GetInstanceByID(cfg.NotifierInstanceID)
		switch {
		case err != nil || notifier == nil:
			errs = append(errs, errors.New("instância de envio dos alertas não encontrada"))
		case !s.isOnline(notifier.Id):
			errs = append(errs, fmt.Errorf("instância de envio %s está desconectada", notifier.Name))
		default:
			for _, n := range numbers {
				if _, err := s.sendService.SendText(&send_service.TextStruct{Number: n, Text: a.Message}, notifier); err != nil {
					errs = append(errs, fmt.Errorf("whatsapp %s: %w", n, err))
				}
			}
		}
	}
	if cfg.WebhookURL != "" {
		if err := s.postWebhook(cfg.WebhookURL, a); err != nil {
			errs = append(errs, fmt.Errorf("webhook: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (s *monitoringService) postWebhook(url string, a Alert) error {
	body, err := json.Marshal(map[string]interface{}{"event": "MonitoringAlert", "data": a})
	if err != nil {
		return err
	}
	resp, err := s.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %s", resp.Status)
	}
	return nil
}

func splitNumbers(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, part)
		if digits != "" {
			out = append(out, digits)
		}
	}
	return out
}

func alertMessage(a Alert, loc *time.Location) string {
	switch a.Type {
	case AlertInstanceDown:
		return fmt.Sprintf("⚠️ *Evolution GO*: a instância *%s* está desconectada desde %s (%s).",
			a.InstanceName, a.Since.In(loc).Format("02/01 15:04"), reasonText(a.Reason, a.Detail))
	case AlertLoggedOut:
		return fmt.Sprintf("🚨 *Evolution GO*: a instância *%s* foi desconectada do WhatsApp (logout). É preciso ler o QR Code de novo no manager.",
			a.InstanceName)
	case AlertBanned:
		return fmt.Sprintf("🚨 *Evolution GO*: o WhatsApp bloqueou temporariamente a instância *%s* (%s).", a.InstanceName, a.Detail)
	case AlertRecovered:
		return fmt.Sprintf("✅ *Evolution GO*: a instância *%s* voltou a se conectar depois de %s fora do ar.",
			a.InstanceName, formatDuration(a.At.Sub(a.Since)))
	case AlertFailures:
		return fmt.Sprintf("⚠️ *Evolution GO*: %d falhas de entrega (%s) nos últimos %d minutos na instância *%s*.",
			a.Count, componentName(a.Reason), int(FailureWindow.Minutes()), a.InstanceName)
	case AlertTest:
		return "✅ *Evolution GO*: alerta de teste. As notificações de monitoramento estão funcionando."
	}
	return a.Type
}

func reasonText(kind, detail string) string {
	switch kind {
	case monitoring_model.KindDisconnected:
		return "conexão encerrada pelo WhatsApp"
	case monitoring_model.KindConnectFailure:
		return "falha ao conectar: " + detail
	case monitoring_model.KindStreamReplaced:
		return "sessão aberta em outro lugar"
	case monitoring_model.KindServiceStart:
		return "não reconectou após o reinício do Evolution GO"
	case monitoring_model.KindLoggedOut:
		return "logout"
	case monitoring_model.KindTemporaryBan:
		return "bloqueio temporário: " + detail
	}
	return kind
}

func componentName(c string) string {
	switch c {
	case "send":
		return "envio pelo WhatsApp"
	case "rabbitmq":
		return "RabbitMQ"
	case "nats":
		return "NATS"
	case "chatwoot":
		return "Chatwoot"
	case "typebot":
		return "Typebot"
	}
	return c
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "menos de 1 min"
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	if h == 0 {
		return fmt.Sprintf("%d min", m)
	}
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dmin", h, m)
}
