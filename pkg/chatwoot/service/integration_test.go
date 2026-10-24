package chatwoot_service

// Integration tests against a real Chatwoot. Skipped unless configured:
//
//	CHATWOOT_TEST_URL=http://localhost:3000
//	CHATWOOT_TEST_ACCOUNT=1
//	CHATWOOT_TEST_TOKEN=<admin access token>
//	CHATWOOT_TEST_INBOX=<api inbox id>
//	CHATWOOT_TEST_DB=postgres://...   (optional, history import)
//	CHATWOOT_TEST_EXPECT_WEBHOOK_NOTE=1 (optional: inbox webhook points to a running
//	  Evolution GO whose instance is disconnected, which must answer with a private note)

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickmn/go-cache"
	"go.mau.fi/whatsmeow/types"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	"github.com/evolution-foundation/evolution-go/pkg/config"
	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
)

type memRepo struct {
	mu   sync.Mutex
	msgs []chatwoot_model.ChatwootMessage
}

func (r *memRepo) GetConfig(string) (*chatwoot_model.ChatwootConfig, error)    { return nil, nil }
func (r *memRepo) GetEnabledConfigs() ([]chatwoot_model.ChatwootConfig, error) { return nil, nil }
func (r *memRepo) SaveConfig(*chatwoot_model.ChatwootConfig) error             { return nil }
func (r *memRepo) DeleteConfig(string) error                                   { return nil }
func (r *memRepo) DeleteMessagesByInstance(string) error                       { return nil }
func (r *memRepo) SaveMessage(m *chatwoot_model.ChatwootMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, *m)
	return nil
}
func (r *memRepo) GetMessageByWaID(inst, id string) (*chatwoot_model.ChatwootMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.msgs {
		if r.msgs[i].InstanceID == inst && r.msgs[i].WaMessageID == id {
			return &r.msgs[i], nil
		}
	}
	return nil, nil
}
func (r *memRepo) GetMessageByChatwootID(inst string, id int) (*chatwoot_model.ChatwootMessage, error) {
	msgs, _ := r.GetMessagesByChatwootID(inst, id)
	if len(msgs) == 0 {
		return nil, nil
	}
	return &msgs[0], nil
}
func (r *memRepo) GetMessagesByChatwootID(inst string, id int) ([]chatwoot_model.ChatwootMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []chatwoot_model.ChatwootMessage
	for _, m := range r.msgs {
		if m.InstanceID == inst && m.ChatwootMessageID == id {
			out = append(out, m)
		}
	}
	return out, nil
}

func integrationEnv(t *testing.T) (*chatwoot_model.ChatwootConfig, *chatwootService) {
	t.Helper()
	baseURL := os.Getenv("CHATWOOT_TEST_URL")
	if baseURL == "" {
		t.Skip("CHATWOOT_TEST_URL not set")
	}
	account, _ := strconv.Atoi(os.Getenv("CHATWOOT_TEST_ACCOUNT"))
	inbox, _ := strconv.Atoi(os.Getenv("CHATWOOT_TEST_INBOX"))
	cfg := &chatwoot_model.ChatwootConfig{
		InstanceID:          "00000000-0000-0000-0000-000000000001",
		Enabled:             true,
		URL:                 baseURL,
		AccountID:           account,
		Token:               os.Getenv("CHATWOOT_TEST_TOKEN"),
		InboxID:             inbox,
		ReopenConversation:  true,
		MergeBrazilContacts: true,
		ImportMessages:      true,
		DaysLimitImport:     3,
	}
	if ibs, err := newAPIClient(cfg.URL, cfg.AccountID, cfg.Token).ListInboxes(context.Background()); err == nil {
		for _, ib := range ibs {
			if ib.ID == inbox {
				cfg.InboxIdentifier = ib.InboxIdentifier
			}
		}
	}
	svc := &chatwootService{
		repo:          &memRepo{},
		loggerWrapper: logger_wrapper.NewLoggerManager(&config.Config{LogDirectory: t.TempDir(), LogMaxSize: 1, LogMaxBackups: 1, LogMaxAge: 1}),
		importDBURI:   os.Getenv("CHATWOOT_TEST_DB"),
		cfgCache:      cache.New(time.Minute, time.Minute),
		convCache:     cache.New(time.Minute, time.Minute),
		pending:       cache.New(time.Minute, time.Minute),
		throttle:      cache.New(time.Minute, time.Minute),
		pools:         map[string][]chan func(){},
	}
	return cfg, svc
}

type cwListedMessage struct {
	ID                int                    `json:"id"`
	Content           string                 `json:"content"`
	MessageType       int                    `json:"message_type"`
	Private           bool                   `json:"private"`
	SourceID          string                 `json:"source_id"`
	ContentAttributes map[string]interface{} `json:"content_attributes"`
	Attachments       []struct {
		FileType string `json:"file_type"`
	} `json:"attachments"`
}

func listMessages(t *testing.T, cfg *chatwoot_model.ChatwootConfig, conv int) []cwListedMessage {
	t.Helper()
	var out struct {
		Payload []cwListedMessage `json:"payload"`
	}
	api := newAPIClient(cfg.URL, cfg.AccountID, cfg.Token)
	if err := api.doJSON(context.Background(), http.MethodGet, api.accountPath("/conversations/%d/messages", conv), nil, &out); err != nil {
		t.Fatal(err)
	}
	return out.Payload
}

func TestIntegrationConversationAndMessages(t *testing.T) {
	cfg, svc := integrationEnv(t)
	ctx := context.Background()
	api := svc.api(cfg)

	// Unique number per run: 55 + DDD 11 + 9 + 8 digits
	suffix := fmt.Sprintf("%08d", time.Now().UnixNano()%100000000)
	jid := types.NewJID("55119"+suffix, types.DefaultUserServer)
	jidNo9 := types.NewJID("5511"+suffix, types.DefaultUserServer)

	conv, err := svc.ensureConversation(ctx, cfg, cfg.InstanceID, nil, jid, "Fulano Teste")
	if err != nil {
		t.Fatalf("ensureConversation: %v", err)
	}
	if conv.ConversationID == 0 || conv.SourceID == "" {
		t.Fatalf("bad conv ref: %+v", conv)
	}

	// Found again through the search API, and through the Brazilian variant.
	svc.convCache.Flush()
	again, err := svc.ensureConversation(ctx, cfg, cfg.InstanceID, nil, jid, "")
	if err != nil || again.ConversationID != conv.ConversationID || again.ContactID != conv.ContactID {
		t.Fatalf("second lookup = %+v, %v; want %+v", again, err, conv)
	}
	svc.convCache.Flush()
	variant, err := svc.ensureConversation(ctx, cfg, cfg.InstanceID, nil, jidNo9, "")
	if err != nil || variant.ContactID != conv.ContactID {
		t.Fatalf("BR variant lookup = %+v, %v; want contact %d", variant, err, conv.ContactID)
	}

	// Text message
	text, err := api.CreateMessage(ctx, conv.ConversationID, cwCreateMessage{
		Content: "olá do WhatsApp", MessageType: "incoming", SourceID: sourceIDPrefix + "TXT" + suffix,
	})
	if err != nil {
		t.Fatalf("create text: %v", err)
	}

	// Attachment replying to the text
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	media, err := api.CreateMessage(ctx, conv.ConversationID, cwCreateMessage{
		Content: "legenda", MessageType: "incoming", SourceID: sourceIDPrefix + "IMG" + suffix,
		ContentAttributes: map[string]interface{}{"in_reply_to": text.ID},
		Attachment:        &cwAttachment{FileName: "foto.png", MimeType: "image/png", Data: buf.Bytes()},
	})
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}

	// Outgoing (sent from the phone) with a JSON reply
	out, err := api.CreateMessage(ctx, conv.ConversationID, cwCreateMessage{
		Content: "resposta do celular", MessageType: "outgoing", SourceID: sourceIDPrefix + "OUT" + suffix,
		ContentAttributes: map[string]interface{}{"in_reply_to": text.ID},
	})
	if err != nil {
		t.Fatalf("create outgoing: %v", err)
	}

	msgs := listMessages(t, cfg, conv.ConversationID)
	byID := map[int]cwListedMessage{}
	for _, m := range msgs {
		byID[m.ID] = m
	}
	if m := byID[text.ID]; m.SourceID != sourceIDPrefix+"TXT"+suffix || m.MessageType != 0 {
		t.Errorf("text message = %+v", m)
	}
	if m := byID[media.ID]; len(m.Attachments) != 1 || m.Attachments[0].FileType != "image" || fmt.Sprint(m.ContentAttributes["in_reply_to"]) != strconv.Itoa(text.ID) {
		t.Errorf("attachment message = %+v", m)
	}
	if m := byID[out.ID]; m.MessageType != 1 || fmt.Sprint(m.ContentAttributes["in_reply_to"]) != strconv.Itoa(text.ID) {
		t.Errorf("outgoing message = %+v", m)
	}

	// Delete (WhatsApp revoke)
	if err := api.DeleteMessage(ctx, conv.ConversationID, out.ID); err != nil {
		t.Errorf("delete: %v", err)
	}

	// Contact read receipt
	if cfg.InboxIdentifier != "" {
		if err := api.UpdateLastSeen(ctx, cfg.InboxIdentifier, conv.SourceID, conv.ConversationID); err != nil {
			t.Errorf("update_last_seen: %v", err)
		}
	}

	// Agent reply -> inbox webhook -> Evolution GO (disconnected) -> private note
	if os.Getenv("CHATWOOT_TEST_EXPECT_WEBHOOK_NOTE") == "1" {
		if _, err := api.CreateMessage(ctx, conv.ConversationID, cwCreateMessage{Content: "**resposta** do agente", MessageType: "outgoing"}); err != nil {
			t.Fatalf("agent reply: %v", err)
		}
		deadline := time.Now().Add(45 * time.Second)
		found := false
		for time.Now().Before(deadline) && !found {
			time.Sleep(2 * time.Second)
			for _, m := range listMessages(t, cfg, conv.ConversationID) {
				if m.Private && strings.Contains(m.Content, "WhatsApp") {
					found = true
					t.Logf("private note: %s", m.Content)
				}
			}
		}
		if !found {
			t.Error("no private note: webhook was not delivered/handled")
		}
	}
}

func TestIntegrationHistoryImport(t *testing.T) {
	cfg, svc := integrationEnv(t)
	if svc.importDBURI == "" {
		t.Skip("CHATWOOT_TEST_DB not set")
	}
	db, err := svc.importDatabase()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	userType, userID, err := tokenOwner(ctx, db, cfg.Token)
	if err != nil {
		t.Fatalf("tokenOwner: %v", err)
	}

	suffix := fmt.Sprintf("%08d", time.Now().UnixNano()%100000000)
	jid := types.NewJID("55219"+suffix, types.DefaultUserServer)
	base := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	chat := historyChat{JID: jid, Name: "Histórico Teste", Messages: []historyMessage{
		{WaID: "H1" + suffix, Content: "primeira", Timestamp: base, SenderJid: jid.String()},
		{WaID: "H2" + suffix, Content: "resposta antiga", FromMe: true, Timestamp: base.Add(time.Minute), SenderJid: "me@s.whatsapp.net"},
	}}

	n, err := svc.importChat(ctx, db, cfg, cfg.InstanceID, chat, userType, userID)
	if err != nil || n != 2 {
		t.Fatalf("importChat = %d, %v", n, err)
	}
	// Idempotent
	if n, err = svc.importChat(ctx, db, cfg, cfg.InstanceID, chat, userType, userID); err != nil || n != 0 {
		t.Fatalf("re-import = %d, %v", n, err)
	}

	// The imported conversation is visible through the API with the original timestamps.
	conv, err := svc.ensureConversation(ctx, cfg, cfg.InstanceID, nil, jid, "")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Payload []struct {
			Content   string `json:"content"`
			CreatedAt int64  `json:"created_at"`
			SourceID  string `json:"source_id"`
		} `json:"payload"`
	}
	api := svc.api(cfg)
	if err := api.doJSON(ctx, http.MethodGet, api.accountPath("/conversations/%d/messages", conv.ConversationID), nil, &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, m := range out.Payload {
		got[m.SourceID] = m.CreatedAt
	}
	if got[sourceIDPrefix+"H1"+suffix] != base.Unix() {
		t.Errorf("imported timestamp = %d, want %d (messages: %s)", got[sourceIDPrefix+"H1"+suffix], base.Unix(), mustJSON(out.Payload))
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
