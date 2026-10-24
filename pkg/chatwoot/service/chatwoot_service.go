package chatwoot_service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	chatwoot_repository "github.com/evolution-foundation/evolution-go/pkg/chatwoot/repository"
	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
	instance_repository "github.com/evolution-foundation/evolution-go/pkg/instance/repository"
	"github.com/evolution-foundation/evolution-go/pkg/internal/workqueue"
	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
	send_service "github.com/evolution-foundation/evolution-go/pkg/sendMessage/service"
)

type ChatwootService interface {
	GetConfig(instanceID string) (*chatwoot_model.ChatwootConfig, error)
	SaveConfig(ctx context.Context, instanceID string, input ConfigInput) (*chatwoot_model.ChatwootConfig, error)
	DeleteConfig(instanceID string) error
	WebhookURL(cfg *chatwoot_model.ChatwootConfig) string
	ImportAvailable() bool

	// HandleWebhook receives an event posted by Chatwoot to the inbox webhook.
	HandleWebhook(instanceID, secret string, body []byte) error
	// ImportContacts creates Chatwoot contacts for the WhatsApp address book.
	ImportContacts(instanceID string) error

	// HandleWhatsappEvent implements whatsmeow_service.EventListener.
	HandleWhatsappEvent(instance *instance_model.Instance, client *whatsmeow.Client, evt interface{})

	// HandoffToAgent opens the chat's conversation for the agents (e.g. when a
	// bot flow ends). Implements typebot_service.Handoff.
	HandoffToAgent(instanceID string, chat types.JID)
	// OnConversationResolved registers a callback for conversations resolved in Chatwoot.
	OnConversationResolved(fn func(instanceID string, chat types.JID))
}

// ConfigInput is the payload accepted from the manager to configure the integration.
type ConfigInput struct {
	Enabled             bool   `json:"enabled"`
	URL                 string `json:"url"`
	AccountID           int    `json:"accountId"`
	Token               string `json:"token"`
	InboxName           string `json:"inboxName"`
	AutoCreateInbox     bool   `json:"autoCreateInbox"`
	WebhookBaseURL      string `json:"webhookBaseUrl"`
	SignMessages        bool   `json:"signMessages"`
	SignDelimiter       string `json:"signDelimiter"`
	ReopenConversation  bool   `json:"reopenConversation"`
	ConversationPending bool   `json:"conversationPending"`
	MergeBrazilContacts bool   `json:"mergeBrazilContacts"`
	IgnoreJids          string `json:"ignoreJids"`
	ImportContacts      bool   `json:"importContacts"`
	ImportMessages      bool   `json:"importMessages"`
	DaysLimitImport     int    `json:"daysLimitImport"`
}

type convRef struct {
	ContactID      int
	ConversationID int
	SourceID       string
}

type chatwootService struct {
	repo          chatwoot_repository.ChatwootRepository
	instanceRepo  instance_repository.InstanceRepository
	sendService   send_service.SendService
	clientPointer map[string]*whatsmeow.Client
	loggerWrapper *logger_wrapper.LoggerManager
	importDBURI   string

	importOnce sync.Once
	importDB   *sql.DB
	importErr  error

	cfgCache  *cache.Cache // instanceID -> *ChatwootConfig (Enabled=false when absent)
	convCache *cache.Cache // instanceID|chatJID -> *convRef
	pending   *cache.Cache // instanceID|waMessageID -> sent from Chatwoot, skip echo
	throttle  *cache.Cache // generic once-per-interval keys

	convLocks sync.Map // instanceID|chatJID -> *sync.Mutex

	queue *workqueue.Queue

	onResolved func(instanceID string, chat types.JID)
}

const workersPerInstance = 4

func NewChatwootService(
	repo chatwoot_repository.ChatwootRepository,
	instanceRepo instance_repository.InstanceRepository,
	sendService send_service.SendService,
	clientPointer map[string]*whatsmeow.Client,
	loggerWrapper *logger_wrapper.LoggerManager,
	importDBURI string,
) ChatwootService {
	return &chatwootService{
		repo:          repo,
		instanceRepo:  instanceRepo,
		sendService:   sendService,
		clientPointer: clientPointer,
		loggerWrapper: loggerWrapper,
		importDBURI:   importDBURI,
		cfgCache:      cache.New(30*time.Second, time.Minute),
		convCache:     cache.New(30*time.Minute, 10*time.Minute),
		pending:       cache.New(10*time.Minute, 5*time.Minute),
		throttle:      cache.New(time.Minute, 5*time.Minute),
		queue:         newQueue(loggerWrapper),
	}
}

// ---- Configuration ----

func (s *chatwootService) GetConfig(instanceID string) (*chatwoot_model.ChatwootConfig, error) {
	return s.repo.GetConfig(instanceID)
}

func (s *chatwootService) ImportAvailable() bool {
	return s.importDBURI != ""
}

func (s *chatwootService) WebhookURL(cfg *chatwoot_model.ChatwootConfig) string {
	if cfg == nil || cfg.WebhookBaseURL == "" || cfg.WebhookSecret == "" {
		return ""
	}
	return fmt.Sprintf("%s/chatwoot/webhook/%s/%s", strings.TrimRight(cfg.WebhookBaseURL, "/"), cfg.InstanceID, cfg.WebhookSecret)
}

func (s *chatwootService) SaveConfig(ctx context.Context, instanceID string, in ConfigInput) (*chatwoot_model.ChatwootConfig, error) {
	cfg, err := s.repo.GetConfig(instanceID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		// Save() with a preset primary key skips autoCreateTime; set it here.
		cfg = &chatwoot_model.ChatwootConfig{InstanceID: instanceID, CreatedAt: time.Now()}
	}
	if cfg.WebhookSecret == "" {
		cfg.WebhookSecret = randomHex(24)
	}

	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	in.WebhookBaseURL = strings.TrimRight(strings.TrimSpace(in.WebhookBaseURL), "/")
	in.InboxName = strings.TrimSpace(in.InboxName)
	in.Token = strings.TrimSpace(in.Token)
	if in.DaysLimitImport <= 0 {
		in.DaysLimitImport = 3
	}

	// An existing connection might point to a different inbox; reset it when
	// the account or inbox name changes.
	if cfg.URL != in.URL || cfg.AccountID != in.AccountID || !strings.EqualFold(cfg.InboxName, in.InboxName) {
		cfg.InboxID = 0
		cfg.InboxIdentifier = ""
	}

	cfg.Enabled = in.Enabled
	cfg.URL = in.URL
	cfg.AccountID = in.AccountID
	cfg.Token = in.Token
	cfg.InboxName = in.InboxName
	cfg.AutoCreateInbox = in.AutoCreateInbox
	cfg.WebhookBaseURL = in.WebhookBaseURL
	cfg.SignMessages = in.SignMessages
	cfg.SignDelimiter = in.SignDelimiter
	cfg.ReopenConversation = in.ReopenConversation
	cfg.ConversationPending = in.ConversationPending
	cfg.MergeBrazilContacts = in.MergeBrazilContacts
	cfg.IgnoreJids = in.IgnoreJids
	cfg.ImportContacts = in.ImportContacts
	cfg.ImportMessages = in.ImportMessages
	cfg.DaysLimitImport = in.DaysLimitImport

	if cfg.Enabled {
		if err := validateConfig(cfg); err != nil {
			return nil, err
		}
		if err := s.syncInbox(ctx, cfg); err != nil {
			return nil, err
		}
	}

	if err := s.repo.SaveConfig(cfg); err != nil {
		return nil, err
	}
	s.invalidateInstance(instanceID)
	return cfg, nil
}

func (s *chatwootService) DeleteConfig(instanceID string) error {
	if err := s.repo.DeleteConfig(instanceID); err != nil {
		return err
	}
	if err := s.repo.DeleteMessagesByInstance(instanceID); err != nil {
		return err
	}
	s.invalidateInstance(instanceID)
	return nil
}

func validateConfig(cfg *chatwoot_model.ChatwootConfig) error {
	if u, err := url.Parse(cfg.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("URL do Chatwoot inválida")
	}
	if cfg.AccountID <= 0 {
		return errors.New("ID da conta do Chatwoot é obrigatório")
	}
	if cfg.Token == "" {
		return errors.New("token de acesso do Chatwoot é obrigatório")
	}
	if cfg.InboxName == "" {
		return errors.New("nome da caixa de entrada é obrigatório")
	}
	if u, err := url.Parse(cfg.WebhookBaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("URL pública do Evolution GO inválida")
	}
	return nil
}

// syncInbox finds (or creates) the API inbox and points its webhook at us.
func (s *chatwootService) syncInbox(ctx context.Context, cfg *chatwoot_model.ChatwootConfig) error {
	api := s.api(cfg)
	webhookURL := s.WebhookURL(cfg)

	inboxes, err := api.ListInboxes(ctx)
	if err != nil {
		return friendlyAPIError(err)
	}

	var inbox *cwInbox
	for i := range inboxes {
		if strings.EqualFold(inboxes[i].Name, cfg.InboxName) && inboxes[i].ChannelType == "Channel::Api" {
			inbox = &inboxes[i]
			break
		}
	}

	if inbox == nil {
		if !cfg.AutoCreateInbox {
			return fmt.Errorf("caixa de entrada %q (tipo API) não encontrada no Chatwoot", cfg.InboxName)
		}
		created, err := api.CreateInbox(ctx, cfg.InboxName, webhookURL)
		if err != nil {
			return friendlyAPIError(err)
		}
		inbox = created
		if inbox.InboxIdentifier == "" {
			// Some Chatwoot versions omit the identifier on create; re-read it.
			if all, err := api.ListInboxes(ctx); err == nil {
				for _, ib := range all {
					if ib.ID == inbox.ID {
						inbox.InboxIdentifier = ib.InboxIdentifier
					}
				}
			}
		}
	} else if inbox.WebhookURL != webhookURL {
		if err := api.UpdateInboxWebhook(ctx, inbox.ID, webhookURL); err != nil {
			return friendlyAPIError(err)
		}
	}

	cfg.InboxID = inbox.ID
	cfg.InboxIdentifier = inbox.InboxIdentifier
	return nil
}

func friendlyAPIError(err error) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 401:
			return errors.New("token do Chatwoot inválido ou sem permissão")
		case 403:
			return errors.New("o token não tem permissão de administrador nesta conta do Chatwoot")
		}
	}
	if errors.Is(err, ErrNotFound) {
		return errors.New("conta do Chatwoot não encontrada (verifique a URL e o ID da conta)")
	}
	return fmt.Errorf("falha ao falar com o Chatwoot: %w", err)
}

func (s *chatwootService) api(cfg *chatwoot_model.ChatwootConfig) *apiClient {
	return newAPIClient(cfg.URL, cfg.AccountID, cfg.Token)
}

// activeConfig returns the config if the integration is enabled and ready.
func (s *chatwootService) activeConfig(instanceID string) *chatwoot_model.ChatwootConfig {
	if v, ok := s.cfgCache.Get(instanceID); ok {
		cfg := v.(*chatwoot_model.ChatwootConfig)
		if cfg.Enabled && cfg.InboxID > 0 {
			return cfg
		}
		return nil
	}

	cfg, err := s.repo.GetConfig(instanceID)
	if err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogError("[%s] [chatwoot] failed to load config: %v", instanceID, err)
		return nil
	}
	if cfg == nil {
		cfg = &chatwoot_model.ChatwootConfig{InstanceID: instanceID}
	}
	s.cfgCache.Set(instanceID, cfg, cache.DefaultExpiration)
	if cfg.Enabled && cfg.InboxID > 0 {
		return cfg
	}
	return nil
}

func (s *chatwootService) invalidateInstance(instanceID string) {
	s.cfgCache.Delete(instanceID)
	prefix := instanceID + "|"
	for k := range s.convCache.Items() {
		if strings.HasPrefix(k, prefix) {
			s.convCache.Delete(k)
		}
	}
}

// enqueue runs fn on one of the instance workers, chosen by key, so events of
// the same chat are processed in order without blocking the whatsmeow event
// loop or the HTTP handler.
func (s *chatwootService) enqueue(instanceID, key string, fn func()) {
	s.queue.Enqueue(instanceID, key, fn)
}

func newQueue(loggerWrapper *logger_wrapper.LoggerManager) *workqueue.Queue {
	return workqueue.New(workersPerInstance,
		func(instanceID string, r interface{}) {
			loggerWrapper.GetLogger(instanceID).LogError("[%s] [chatwoot] panic: %v", instanceID, r)
		},
		func(instanceID string) {
			loggerWrapper.GetLogger(instanceID).LogWarn("[%s] [chatwoot] queue full, processing out of order", instanceID)
		})
}

func (s *chatwootService) OnConversationResolved(fn func(instanceID string, chat types.JID)) {
	s.onResolved = fn
}

func (s *chatwootService) HandoffToAgent(instanceID string, chat types.JID) {
	cfg := s.activeConfig(instanceID)
	if cfg == nil {
		return
	}
	chat = chat.ToNonAD()
	s.enqueue(instanceID, chat.String(), func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conv, err := s.ensureConversation(ctx, cfg, instanceID, s.clientPointer[instanceID], chat, "")
		if err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogError("[%s] [chatwoot] handoff: %v", instanceID, err)
			return
		}
		if err := s.api(cfg).ToggleConversationStatus(ctx, conv.ConversationID, "open"); err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogError("[%s] [chatwoot] handoff: failed to open conversation %d: %v", instanceID, conv.ConversationID, err)
			return
		}
		s.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] [chatwoot] conversation %d handed off to agents", instanceID, conv.ConversationID)
	})
}

func (s *chatwootService) convLock(key string) *sync.Mutex {
	v, _ := s.convLocks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
