package typebot_service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
	instance_repository "github.com/evolution-foundation/evolution-go/pkg/instance/repository"
	"github.com/evolution-foundation/evolution-go/pkg/internal/workqueue"
	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
	send_service "github.com/evolution-foundation/evolution-go/pkg/sendMessage/service"
	typebot_model "github.com/evolution-foundation/evolution-go/pkg/typebot/model"
	typebot_repository "github.com/evolution-foundation/evolution-go/pkg/typebot/repository"
)

// Handoff moves a conversation to the human agents (implemented by the
// Chatwoot integration). It is optional.
type Handoff interface {
	HandoffToAgent(instanceID string, chat types.JID)
}

type TypebotService interface {
	GetConfig(instanceID string) (*typebot_model.TypebotConfig, error)
	SaveConfig(ctx context.Context, instanceID string, input ConfigInput) (*typebot_model.TypebotConfig, error)
	DeleteConfig(instanceID string) error
	ListSessions(instanceID string) ([]typebot_model.TypebotSession, error)
	CloseSessions(instanceID string) error

	SetHandoff(h Handoff)
	// CloseSession ends the bot session of a chat (e.g. its Chatwoot conversation was resolved).
	CloseSession(instanceID string, chat types.JID)

	// HandleWhatsappEvent implements whatsmeow_service.EventListener.
	HandleWhatsappEvent(instance *instance_model.Instance, client *whatsmeow.Client, evt interface{})
}

// ConfigInput is the payload accepted from the manager.
type ConfigInput struct {
	Enabled         bool   `json:"enabled"`
	URL             string `json:"url"`
	PublicID        string `json:"publicId"`
	TriggerType     string `json:"triggerType"`
	TriggerKeywords string `json:"triggerKeywords"`
	TriggerOperator string `json:"triggerOperator"`
	ExpireMinutes   int    `json:"expireMinutes"`
	KeywordFinish   string `json:"keywordFinish"`
	DelayMessageMs  int    `json:"delayMessageMs"`
	UnknownMessage  string `json:"unknownMessage"`
	StopBotFromMe   bool   `json:"stopBotFromMe"`
	HandoffChatwoot bool   `json:"handoffChatwoot"`
	IgnoreJids      string `json:"ignoreJids"`
}

const DefaultUnknownMessage = "Não entendi 🤔 Responda com o *número* de uma das opções:"

type typebotService struct {
	repo          typebot_repository.TypebotRepository
	instanceRepo  instance_repository.InstanceRepository
	sendService   send_service.SendService
	clientPointer map[string]*whatsmeow.Client
	loggerWrapper *logger_wrapper.LoggerManager
	handoff       Handoff

	cfgCache *cache.Cache // instanceID -> *TypebotConfig (Enabled=false when absent)
	botSent  *cache.Cache // instanceID|waMessageID -> sent by the bot
	queue    *workqueue.Queue
}

func NewTypebotService(
	repo typebot_repository.TypebotRepository,
	instanceRepo instance_repository.InstanceRepository,
	sendService send_service.SendService,
	clientPointer map[string]*whatsmeow.Client,
	loggerWrapper *logger_wrapper.LoggerManager,
) TypebotService {
	return &typebotService{
		repo:          repo,
		instanceRepo:  instanceRepo,
		sendService:   sendService,
		clientPointer: clientPointer,
		loggerWrapper: loggerWrapper,
		cfgCache:      cache.New(30*time.Second, time.Minute),
		botSent:       cache.New(10*time.Minute, 5*time.Minute),
		queue: workqueue.New(4,
			func(instanceID string, r interface{}) {
				loggerWrapper.GetLogger(instanceID).LogError("[%s] [typebot] panic: %v", instanceID, r)
			},
			func(instanceID string) {
				loggerWrapper.GetLogger(instanceID).LogWarn("[%s] [typebot] queue full, processing out of order", instanceID)
			}),
	}
}

func (s *typebotService) SetHandoff(h Handoff) { s.handoff = h }

// ---- Configuration ----

func (s *typebotService) GetConfig(instanceID string) (*typebot_model.TypebotConfig, error) {
	return s.repo.GetConfig(instanceID)
}

func (s *typebotService) SaveConfig(ctx context.Context, instanceID string, in ConfigInput) (*typebot_model.TypebotConfig, error) {
	cfg, err := s.repo.GetConfig(instanceID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = &typebot_model.TypebotConfig{InstanceID: instanceID, CreatedAt: time.Now()}
	}

	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	in.PublicID = strings.Trim(strings.TrimSpace(in.PublicID), "/")
	// Accept the full share link in the bot field.
	if strings.HasPrefix(in.PublicID, "http") {
		if u, err := url.Parse(in.PublicID); err == nil {
			if in.URL == "" {
				in.URL = u.Scheme + "://" + u.Host
			}
			in.PublicID = strings.Trim(u.Path, "/")
		}
	}
	switch in.TriggerType {
	case typebot_model.TriggerAll, typebot_model.TriggerKeyword, typebot_model.TriggerNone:
	default:
		in.TriggerType = typebot_model.TriggerKeyword
	}
	switch in.TriggerOperator {
	case typebot_model.OperatorEquals, typebot_model.OperatorContains, typebot_model.OperatorStartsWith:
	default:
		in.TriggerOperator = typebot_model.OperatorEquals
	}
	if in.ExpireMinutes < 0 {
		in.ExpireMinutes = 0
	}
	if in.DelayMessageMs < 0 {
		in.DelayMessageMs = 0
	}
	if in.DelayMessageMs > 10000 {
		in.DelayMessageMs = 10000
	}
	if strings.TrimSpace(in.UnknownMessage) == "" {
		in.UnknownMessage = DefaultUnknownMessage
	}

	cfg.Enabled = in.Enabled
	cfg.URL = in.URL
	cfg.PublicID = in.PublicID
	cfg.TriggerType = in.TriggerType
	cfg.TriggerKeywords = in.TriggerKeywords
	cfg.TriggerOperator = in.TriggerOperator
	cfg.ExpireMinutes = in.ExpireMinutes
	cfg.KeywordFinish = strings.TrimSpace(in.KeywordFinish)
	cfg.DelayMessageMs = in.DelayMessageMs
	cfg.UnknownMessage = in.UnknownMessage
	cfg.StopBotFromMe = in.StopBotFromMe
	cfg.HandoffChatwoot = in.HandoffChatwoot
	cfg.IgnoreJids = in.IgnoreJids

	if cfg.Enabled {
		if u, err := url.Parse(cfg.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, errors.New("URL do viewer do Typebot inválida")
		}
		if cfg.PublicID == "" {
			return nil, errors.New("informe o nome público do bot")
		}
		if cfg.TriggerType == typebot_model.TriggerKeyword && strings.TrimSpace(cfg.TriggerKeywords) == "" {
			return nil, errors.New("informe ao menos uma palavra-chave de início")
		}
		checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := newViewerClient(cfg.URL).CheckBot(checkCtx, cfg.PublicID); err != nil {
			return nil, errors.New("não foi possível acessar o bot: " + err.Error())
		}
	}

	if err := s.repo.SaveConfig(cfg); err != nil {
		return nil, err
	}
	s.cfgCache.Delete(instanceID)
	return cfg, nil
}

func (s *typebotService) DeleteConfig(instanceID string) error {
	if err := s.repo.DeleteConfig(instanceID); err != nil {
		return err
	}
	if err := s.repo.DeleteSessions(instanceID); err != nil {
		return err
	}
	s.cfgCache.Delete(instanceID)
	return nil
}

func (s *typebotService) ListSessions(instanceID string) ([]typebot_model.TypebotSession, error) {
	return s.repo.ListSessions(instanceID)
}

func (s *typebotService) CloseSessions(instanceID string) error {
	return s.repo.DeleteSessions(instanceID)
}

func (s *typebotService) CloseSession(instanceID string, chat types.JID) {
	key := chat.ToNonAD().String()
	s.queue.Enqueue(instanceID, key, func() {
		_ = s.repo.DeleteSession(instanceID, key)
	})
}

func (s *typebotService) activeConfig(instanceID string) *typebot_model.TypebotConfig {
	if v, ok := s.cfgCache.Get(instanceID); ok {
		cfg := v.(*typebot_model.TypebotConfig)
		if cfg.Enabled {
			return cfg
		}
		return nil
	}
	cfg, err := s.repo.GetConfig(instanceID)
	if err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogError("[%s] [typebot] failed to load config: %v", instanceID, err)
		return nil
	}
	if cfg == nil {
		cfg = &typebot_model.TypebotConfig{InstanceID: instanceID}
	}
	s.cfgCache.Set(instanceID, cfg, cache.DefaultExpiration)
	if cfg.Enabled {
		return cfg
	}
	return nil
}

// ---- Events ----

func (s *typebotService) HandleWhatsappEvent(instance *instance_model.Instance, client *whatsmeow.Client, rawEvt interface{}) {
	evt, ok := rawEvt.(*events.Message)
	if !ok || instance == nil || client == nil {
		return
	}
	cfg := s.activeConfig(instance.Id)
	if cfg == nil {
		return
	}
	chat := evt.Info.Chat.ToNonAD()
	if chat.Server != types.DefaultUserServer && chat.Server != types.HiddenUserServer {
		return // groups, status, newsletters
	}
	if isIgnored(cfg.IgnoreJids, chat) {
		return
	}
	if evt.Message.GetProtocolMessage() != nil || evt.Message.GetReactionMessage() != nil {
		return
	}
	if evt.Info.IsFromMe {
		if _, ours := s.botSent.Get(instance.Id + "|" + evt.Info.ID); ours {
			return
		}
	}

	s.queue.Enqueue(instance.Id, chat.String(), func() {
		if evt.Info.IsFromMe {
			s.handleHumanMessage(cfg, instance.Id, chat, evt)
		} else {
			s.handleContactMessage(cfg, instance.Id, client, chat, evt)
		}
	})
}

// handleHumanMessage: a message sent by the phone, Chatwoot or the API.
func (s *typebotService) handleHumanMessage(cfg *typebot_model.TypebotConfig, instanceID string, chat types.JID, evt *events.Message) {
	session, err := s.repo.GetSession(instanceID, chat.String())
	if err != nil || session == nil {
		return
	}
	if isKeyword(incomingText(evt.Message), cfg.KeywordFinish) {
		_ = s.repo.DeleteSession(instanceID, chat.String())
		return
	}
	if cfg.StopBotFromMe && session.Status == typebot_model.SessionOpened {
		session.Status = typebot_model.SessionPaused
		session.UpdatedAt = time.Now()
		if err := s.repo.SaveSession(session); err == nil {
			s.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] [typebot] bot paused for %s (human reply)", instanceID, chat.User)
		}
	}
}

func (s *typebotService) handleContactMessage(cfg *typebot_model.TypebotConfig, instanceID string, client *whatsmeow.Client, chat types.JID, evt *events.Message) {
	log := s.loggerWrapper.GetLogger(instanceID)
	text := strings.TrimSpace(incomingText(evt.Message))
	if text == "" {
		return // audio, stickers, media without caption
	}

	session, err := s.repo.GetSession(instanceID, chat.String())
	if err != nil {
		log.LogError("[%s] [typebot] failed to load session: %v", instanceID, err)
		return
	}

	if isKeyword(text, cfg.KeywordFinish) {
		if session != nil {
			_ = s.repo.DeleteSession(instanceID, chat.String())
		}
		return
	}

	if session != nil && cfg.ExpireMinutes > 0 && time.Since(session.UpdatedAt) > time.Duration(cfg.ExpireMinutes)*time.Minute {
		_ = s.repo.DeleteSession(instanceID, chat.String())
		session = nil
	}

	if session != nil && session.Status == typebot_model.SessionPaused {
		return // a human is handling this chat
	}

	viewer := newViewerClient(cfg.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if session != nil {
		var input *tbInput
		if session.Input != "" {
			input = &tbInput{}
			if err := json.Unmarshal([]byte(session.Input), input); err != nil {
				input = nil
			}
		}
		answer, ok := matchChoice(input, text)
		if !ok {
			s.send(cfg, instanceID, client, chat, []outItem{{Kind: "text", Text: cfg.UnknownMessage + "\n\n" + formatChoices(input)}})
			s.touch(session)
			return
		}

		resp, err := viewer.ContinueChat(ctx, session.SessionID, answer)
		if err == nil {
			s.deliver(cfg, instanceID, client, chat, session, resp)
			return
		}
		if !errors.Is(err, ErrSessionNotFound) {
			log.LogError("[%s] [typebot] continueChat failed for %s: %v", instanceID, chat.User, err)
			return
		}
		// The session ended on the Typebot side; this message may start a new one.
		_ = s.repo.DeleteSession(instanceID, chat.String())
	}

	if !matchTrigger(cfg, text) {
		return
	}

	resp, err := viewer.StartChat(ctx, cfg.PublicID, s.prefilledVariables(instanceID, chat, evt))
	if err != nil {
		log.LogError("[%s] [typebot] startChat failed for %s: %v", instanceID, chat.User, err)
		return
	}
	log.LogInfo("[%s] [typebot] session started for %s", instanceID, chat.User)
	s.deliver(cfg, instanceID, client, chat, &typebot_model.TypebotSession{
		InstanceID: instanceID,
		RemoteJid:  chat.String(),
		SessionID:  resp.SessionID,
	}, resp)
}

func (s *typebotService) prefilledVariables(instanceID string, chat types.JID, evt *events.Message) map[string]string {
	vars := map[string]string{
		"remoteJid": chat.String(),
		"pushName":  evt.Info.PushName,
	}
	if chat.Server == types.DefaultUserServer {
		vars["phone"] = chat.User
	}
	if inst, err := s.instanceRepo.GetInstanceByID(instanceID); err == nil && inst != nil {
		vars["instanceName"] = inst.Name
	}
	return vars
}

// deliver sends the bot reply and stores (or ends) the session.
func (s *typebotService) deliver(cfg *typebot_model.TypebotConfig, instanceID string, client *whatsmeow.Client, chat types.JID, session *typebot_model.TypebotSession, resp *tbResponse) {
	s.send(cfg, instanceID, client, chat, renderResponse(resp))

	if resp.Input == nil {
		_ = s.repo.DeleteSession(instanceID, chat.String())
		s.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] [typebot] flow finished for %s", instanceID, chat.User)
		if cfg.HandoffChatwoot && s.handoff != nil {
			s.handoff.HandoffToAgent(instanceID, chat)
		}
		return
	}

	session.Status = typebot_model.SessionOpened
	session.Input = string(resp.Input.Raw)
	session.UpdatedAt = time.Now()
	if err := s.repo.SaveSession(session); err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogError("[%s] [typebot] failed to save session: %v", instanceID, err)
	}
}

func (s *typebotService) touch(session *typebot_model.TypebotSession) {
	session.UpdatedAt = time.Now()
	_ = s.repo.SaveSession(session)
}

func (s *typebotService) send(cfg *typebot_model.TypebotConfig, instanceID string, client *whatsmeow.Client, chat types.JID, items []outItem) {
	if len(items) == 0 {
		return
	}
	log := s.loggerWrapper.GetLogger(instanceID)
	instance, err := s.instanceRepo.GetInstanceByID(instanceID)
	if err != nil {
		log.LogError("[%s] [typebot] failed to load instance: %v", instanceID, err)
		return
	}

	number := chat.User
	if chat.Server != types.DefaultUserServer {
		number = chat.String()
	}

	for _, it := range items {
		id := newMessageID(client)
		s.botSent.SetDefault(instanceID+"|"+id, true)

		var err error
		switch it.Kind {
		case "text":
			_, err = s.sendService.SendText(&send_service.TextStruct{Number: number, Text: it.Text, Id: id, Delay: int32(cfg.DelayMessageMs)}, instance)
		case "image", "video", "audio":
			_, err = s.sendService.SendMediaUrl(&send_service.MediaStruct{Number: number, Url: it.URL, Type: it.Kind, Id: id, Delay: int32(cfg.DelayMessageMs)}, instance)
			if err != nil {
				// Fall back to the link so the contact still gets the content.
				fallback := newMessageID(client)
				s.botSent.SetDefault(instanceID+"|"+fallback, true)
				_, err = s.sendService.SendText(&send_service.TextStruct{Number: number, Text: it.URL, Id: fallback}, instance)
			}
		}
		if err != nil {
			log.LogError("[%s] [typebot] failed to send to %s: %v", instanceID, chat.User, err)
			return
		}
		if it.WaitAfter > 0 {
			time.Sleep(it.WaitAfter)
		}
	}
}

func isIgnored(list string, jid types.JID) bool {
	for _, item := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == '\n' || r == ';' || r == ' ' }) {
		item = strings.TrimPrefix(strings.TrimSpace(item), "+")
		if item != "" && (item == jid.User || item == jid.String()) {
			return true
		}
	}
	return false
}

// newMessageID pre-generates the WhatsApp message ID so the echo of the bot's
// own send can be recognized (client is nil only in tests).
func newMessageID(client *whatsmeow.Client) string {
	if client == nil {
		return "TB" + strings.ToUpper(strconv.FormatInt(time.Now().UnixNano(), 36))
	}
	return client.GenerateMessageID()
}
