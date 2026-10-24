package typebot_service

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickmn/go-cache"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/evolution-foundation/evolution-go/pkg/config"
	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
	instance_repository "github.com/evolution-foundation/evolution-go/pkg/instance/repository"
	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
	send_service "github.com/evolution-foundation/evolution-go/pkg/sendMessage/service"
	typebot_model "github.com/evolution-foundation/evolution-go/pkg/typebot/model"
)

type memRepo struct {
	mu       sync.Mutex
	sessions map[string]typebot_model.TypebotSession
}

func (r *memRepo) GetConfig(string) (*typebot_model.TypebotConfig, error) { return nil, nil }
func (r *memRepo) SaveConfig(*typebot_model.TypebotConfig) error          { return nil }
func (r *memRepo) DeleteConfig(string) error                              { return nil }
func (r *memRepo) GetSession(inst, jid string) (*typebot_model.TypebotSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[inst+jid]; ok {
		return &s, nil
	}
	return nil, nil
}
func (r *memRepo) SaveSession(s *typebot_model.TypebotSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.InstanceID+s.RemoteJid] = *s
	return nil
}
func (r *memRepo) DeleteSession(inst, jid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, inst+jid)
	return nil
}
func (r *memRepo) ListSessions(string) ([]typebot_model.TypebotSession, error) { return nil, nil }
func (r *memRepo) DeleteSessions(string) error                                 { return nil }

type fakeInstances struct {
	instance_repository.InstanceRepository
}

func (fakeInstances) GetInstanceByID(id string) (*instance_model.Instance, error) {
	return &instance_model.Instance{Id: id, Name: "teste"}, nil
}

type fakeSender struct {
	send_service.SendService
	mu   sync.Mutex
	sent []string
}

func (f *fakeSender) SendText(d *send_service.TextStruct, _ *instance_model.Instance) (*send_service.MessageSendStruct, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, d.Text)
	return &send_service.MessageSendStruct{}, nil
}

func (f *fakeSender) SendMediaUrl(d *send_service.MediaStruct, _ *instance_model.Instance) (*send_service.MessageSendStruct, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, "["+d.Type+"] "+d.Url)
	return &send_service.MessageSendStruct{}, nil
}

func (f *fakeSender) take() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := strings.Join(f.sent, "\n---\n")
	f.sent = nil
	return out
}

type fakeHandoff struct{ chats []types.JID }

func (h *fakeHandoff) HandoffToAgent(_ string, chat types.JID) { h.chats = append(h.chats, chat) }

func textEvent(chat types.JID, text string, fromMe bool) *events.Message {
	return &events.Message{
		Info:    types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, Sender: chat, IsFromMe: fromMe}, ID: "M" + text, PushName: "Cliente Teste"},
		Message: &waE2E.Message{Conversation: proto.String(text)},
	}
}

func TestIntegrationServiceFlow(t *testing.T) {
	baseURL, bot := os.Getenv("TYPEBOT_TEST_URL"), os.Getenv("TYPEBOT_TEST_BOT")
	if baseURL == "" || bot == "" {
		t.Skip("TYPEBOT_TEST_URL/TYPEBOT_TEST_BOT not set")
	}
	repo := &memRepo{sessions: map[string]typebot_model.TypebotSession{}}
	sender := &fakeSender{}
	handoff := &fakeHandoff{}
	s := &typebotService{
		repo:          repo,
		instanceRepo:  fakeInstances{},
		sendService:   sender,
		loggerWrapper: logger_wrapper.NewLoggerManager(&config.Config{LogDirectory: os.TempDir() + "/evogo-test-logs", LogMaxSize: 1, LogMaxBackups: 1, LogMaxAge: 1}),
		handoff:       handoff,
		cfgCache:      cache.New(time.Minute, time.Minute),
		botSent:       cache.New(time.Minute, time.Minute),
	}
	cfg := &typebot_model.TypebotConfig{
		Enabled: true, URL: baseURL, PublicID: bot,
		TriggerType: typebot_model.TriggerKeyword, TriggerKeywords: "festa", TriggerOperator: typebot_model.OperatorEquals,
		ExpireMinutes: 60, KeywordFinish: "#sair", UnknownMessage: DefaultUnknownMessage,
		StopBotFromMe: true, HandoffChatwoot: true,
	}
	inst := "00000000-0000-0000-0000-000000000002"
	chat := types.NewJID("5511900000001", types.DefaultUserServer)
	say := func(text string) string {
		s.handleContactMessage(cfg, inst, nil, chat, textEvent(chat, text, false))
		return sender.take()
	}
	session := func() *typebot_model.TypebotSession { sess, _ := repo.GetSession(inst, chat.String()); return sess }

	if out := say("oi"); out != "" || session() != nil {
		t.Fatalf("non-trigger message must be ignored, got %q", out)
	}
	out := say("Festa")
	if !strings.Contains(out, "*1.*") || session() == nil {
		t.Fatalf("trigger should start the bot with numbered options, got %q", out)
	}
	if out := say("talvez"); !strings.Contains(out, "Não entendi") || !strings.Contains(out, "*1.*") {
		t.Errorf("invalid option should repeat the options, got %q", out)
	}
	if out := say("1"); out == "" || strings.Contains(out, "Não entendi") {
		t.Errorf("option 1 should advance, got %q", out)
	}

	// A human reply pauses the bot; contact messages are then ignored.
	s.handleHumanMessage(cfg, inst, chat, textEvent(chat, "Oi, aqui é a atendente", true))
	if sess := session(); sess == nil || sess.Status != typebot_model.SessionPaused {
		t.Fatalf("session should be paused: %+v", sess)
	}
	if out := say("Maria"); out != "" {
		t.Errorf("paused bot replied: %q", out)
	}

	// #sair closes it; the keyword starts a new one.
	say("#sair")
	if session() != nil {
		t.Fatal("#sair should close the session")
	}
	say("festa")
	for _, reply := range []string{"1", "Maria Teste", "Tema safari, 20/12, 15h", "1", "1"} {
		say(reply)
		if session() == nil {
			break
		}
	}
	if session() != nil {
		t.Fatalf("flow should have ended, session = %+v", session())
	}
	if len(handoff.chats) != 1 || handoff.chats[0] != chat {
		t.Errorf("handoff = %v", handoff.chats)
	}
}
