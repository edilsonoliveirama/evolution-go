package monitoring_service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
	_ "time/tzdata" // METRICS_TIMEZONE in images without zoneinfo

	"github.com/gomessguii/logger"
	"go.mau.fi/whatsmeow"

	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
	instance_repository "github.com/evolution-foundation/evolution-go/pkg/instance/repository"
	monitoring_model "github.com/evolution-foundation/evolution-go/pkg/monitoring/model"
	monitoring_repository "github.com/evolution-foundation/evolution-go/pkg/monitoring/repository"
	send_service "github.com/evolution-foundation/evolution-go/pkg/sendMessage/service"
)

type MonitoringService interface {
	// HandleWhatsappEvent implements whatsmeow_service.EventListener.
	HandleWhatsappEvent(instance *instance_model.Instance, client *whatsmeow.Client, evt interface{})
	RecordFailure(instanceID, component string)
	// Start launches the writer, pruning and alert goroutines.
	Start()

	Dashboard(days int, instanceID string) (*Dashboard, error)
	Events(instanceID string, limit int) ([]EventView, error)
	WritePrometheus(w io.Writer) error

	GetAlertConfig() *monitoring_model.AlertConfig
	SaveAlertConfig(cfg *monitoring_model.AlertConfig) error
	SendTestAlert() error
}

type monitoringService struct {
	repo               monitoring_repository.MonitoringRepository
	instanceRepository instance_repository.InstanceRepository
	sendService        send_service.SendService
	clientPointer      map[string]*whatsmeow.Client
	clientName         string
	tz                 string
	loc                *time.Location
	retention          time.Duration
	httpClient         *http.Client
	now                func() time.Time

	events   chan monitoring_model.MetricEvent
	dropped  atomic.Int64
	counters *counters
	alerts   *alertEngine
	alertCfg atomic.Pointer[monitoring_model.AlertConfig]
}

func (s *monitoringService) Start() {
	if cfg, err := s.repo.GetAlertConfig(); err != nil {
		logger.LogError("[monitoring] failed to load alert settings: %v", err)
	} else if cfg != nil {
		s.alertCfg.Store(cfg)
	}

	// Paired instances that were online before the restart should come back on
	// their own; alert if they don't.
	now := s.now()
	if instances, err := s.instanceRepository.GetAll(s.clientName); err == nil {
		var expected []string
		for _, inst := range instances {
			if inst.Jid != "" && inst.Connected {
				expected = append(expected, inst.Id)
			}
		}
		s.alerts.markDown(expected, now)
	}
	s.events <- monitoring_model.MetricEvent{Kind: monitoring_model.KindServiceStart, CreatedAt: now}

	go s.flushLoop()
	go s.pruneLoop()
	go s.tickLoop()
	logger.LogInfo("[monitoring] started (timezone %s, event detail kept %d days)", s.tz, int(s.retention.Hours()/24))
}

func (s *monitoringService) isOnline(instanceID string) bool {
	client := s.clientPointer[instanceID]
	return client != nil && client.IsConnected() && client.IsLoggedIn()
}

func (s *monitoringService) GetAlertConfig() *monitoring_model.AlertConfig {
	cfg := *s.alertConfig()
	return &cfg
}

func (s *monitoringService) SaveAlertConfig(cfg *monitoring_model.AlertConfig) error {
	if cfg.DisconnectMinutes < 1 || cfg.DisconnectMinutes > 1440 {
		return errors.New("o tempo de desconexão deve ficar entre 1 e 1440 minutos")
	}
	if cfg.FailureThreshold < 0 || cfg.FailureThreshold > 10000 {
		return errors.New("o limite de falhas deve ficar entre 0 (desligado) e 10000")
	}
	numbers := splitNumbers(cfg.Numbers)
	for _, n := range numbers {
		if len(n) < 10 || len(n) > 15 {
			return fmt.Errorf("número inválido: %s (use DDI + DDD + número)", n)
		}
	}
	cfg.Numbers = strings.Join(numbers, ",")
	if cfg.WebhookURL != "" {
		u, err := url.Parse(cfg.WebhookURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("URL do webhook inválida")
		}
	}
	if len(numbers) > 0 && cfg.NotifierInstanceID == "" {
		return errors.New("escolha a instância que vai enviar os alertas pelo WhatsApp")
	}
	if cfg.NotifierInstanceID != "" {
		if inst, err := s.instanceRepository.GetInstanceByID(cfg.NotifierInstanceID); err != nil || inst == nil {
			return errors.New("instância de envio não encontrada")
		}
	}
	if cfg.Enabled && len(numbers) == 0 && cfg.WebhookURL == "" {
		return errors.New("informe ao menos um número de WhatsApp ou um webhook para receber os alertas")
	}
	cfg.UpdatedAt = s.now()
	if err := s.repo.SaveAlertConfig(cfg); err != nil {
		return err
	}
	saved := *cfg
	s.alertCfg.Store(&saved)
	return nil
}

func (s *monitoringService) SendTestAlert() error {
	cfg := s.alertConfig()
	if splitNumbers(cfg.Numbers) == nil && cfg.WebhookURL == "" {
		return errors.New("nenhum canal de alerta configurado")
	}
	a := Alert{Type: AlertTest, At: s.now()}
	a.Message = alertMessage(a, s.loc)
	return s.send(cfg, a)
}

func NewMonitoringService(
	repo monitoring_repository.MonitoringRepository,
	instanceRepository instance_repository.InstanceRepository,
	sendService send_service.SendService,
	clientPointer map[string]*whatsmeow.Client,
	clientName string,
	timezone string,
	retentionDays int,
) MonitoringService {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		logger.LogWarn("[monitoring] invalid METRICS_TIMEZONE %q, using UTC: %v", timezone, err)
		loc, timezone = time.UTC, "UTC"
	}
	if retentionDays <= 0 {
		retentionDays = 90
	}
	return &monitoringService{
		repo:               repo,
		instanceRepository: instanceRepository,
		sendService:        sendService,
		clientPointer:      clientPointer,
		clientName:         clientName,
		tz:                 timezone,
		loc:                loc,
		retention:          time.Duration(retentionDays) * 24 * time.Hour,
		httpClient:         &http.Client{Timeout: 10 * time.Second},
		now:                time.Now,
		events:             make(chan monitoring_model.MetricEvent, bufferSize),
		counters:           newCounters(),
		alerts:             newAlertEngine(),
	}
}
