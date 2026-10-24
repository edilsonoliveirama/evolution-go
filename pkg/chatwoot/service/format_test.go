package chatwoot_service

import (
	"encoding/json"
	"reflect"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestBrazilVariants(t *testing.T) {
	cases := map[string][]string{
		"+5598946926651": {"+5598946926651", "+559846926651"},
		"+559846926651":  {"+559846926651", "+5598946926651"},
		"+5511312345678": {"+5511312345678"}, // 13 digits but no mobile 9
		"+14155552671":   {"+14155552671"},
	}
	for in, want := range cases {
		if got := brazilVariants(in); !reflect.DeepEqual(got, want) {
			t.Errorf("brazilVariants(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMarkdownToWhatsApp(t *testing.T) {
	cases := map[string]string{
		"**negrito**":         "*negrito*",
		"*itálico*":           "_itálico_",
		"~~riscado~~":         "~riscado~",
		"olá **João**, *ok*?": "olá *João*, _ok_?",
		"2 * 3 = 6":           "2 * 3 = 6",
		"sem formatação":      "sem formatação",
		"**a** e **b**":       "*a* e *b*",
	}
	for in, want := range cases {
		if got := markdownToWhatsApp(in); got != want {
			t.Errorf("markdownToWhatsApp(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSignMessage(t *testing.T) {
	if got := signMessage("oi", "Ana", `\n`); got != "*Ana:*\noi" {
		t.Errorf("got %q", got)
	}
	if got := signMessage("oi", "Ana", " - "); got != "*Ana:* - oi" {
		t.Errorf("got %q", got)
	}
	if got := signMessage("oi", "", `\n`); got != "oi" {
		t.Errorf("got %q", got)
	}
}

func TestIsSupportedChat(t *testing.T) {
	cases := map[string]bool{
		"5598946926651@s.whatsapp.net":  true,
		"123456789@lid":                 true,
		"120363000000000000@g.us":       false,
		"status@broadcast":              false,
		"120363000000000000@newsletter": false,
	}
	for in, want := range cases {
		jid, err := types.ParseJID(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := isSupportedChat(jid); got != want {
			t.Errorf("isSupportedChat(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIgnoreList(t *testing.T) {
	ignore := parseIgnoreList("+5598946926651, 123@lid\n5511999999999")
	jid, _ := types.ParseJID("5598946926651@s.whatsapp.net")
	if !isIgnored(ignore, jid) {
		t.Error("expected number to be ignored")
	}
	lid, _ := types.ParseJID("123@lid")
	if !isIgnored(ignore, lid) {
		t.Error("expected lid to be ignored")
	}
	other, _ := types.ParseJID("5511888888888@s.whatsapp.net")
	if isIgnored(ignore, other) {
		t.Error("unexpected ignore")
	}
}

func TestWebhookPayload(t *testing.T) {
	body := `{
		"event": "message_created", "id": 42, "content": "**oi**",
		"message_type": "outgoing", "private": false, "source_id": null,
		"content_attributes": {"in_reply_to": 7},
		"sender": {"name": "Ana Souza", "available_name": "Ana", "type": "user"},
		"conversation": {"id": 9, "meta": {"sender": {"identifier": "5598946926651@s.whatsapp.net", "phone_number": "+5598946926651"}}},
		"attachments": [{"file_type": "image", "data_url": "https://cw.example/rails/active_storage/blobs/x/foto%20nova.png"}]
	}`
	var hook cwWebhook
	if err := json.Unmarshal([]byte(body), &hook); err != nil {
		t.Fatal(err)
	}
	if hook.MessageType != "outgoing" || hook.ID != 42 || hook.Conversation.ID != 9 {
		t.Fatalf("unexpected parse: %+v", hook)
	}
	jid, err := destinationJID(&hook)
	if err != nil || jid.String() != "5598946926651@s.whatsapp.net" {
		t.Fatalf("destinationJID = %v, %v", jid, err)
	}
	if name := attachmentName(hook.Attachments[0].DataURL, 0); name != "foto nova.png" {
		t.Errorf("attachmentName = %q", name)
	}

	var numeric cwWebhook
	if err := json.Unmarshal([]byte(`{"message_type": 0}`), &numeric); err != nil || numeric.MessageType != "incoming" {
		t.Errorf("numeric message_type = %q, %v", numeric.MessageType, err)
	}
}

func TestDestinationJIDFromPhone(t *testing.T) {
	var hook cwWebhook
	hook.Conversation.Meta.Sender.PhoneNumber = "+55 (98) 94692-6651"
	jid, err := destinationJID(&hook)
	if err != nil || jid.String() != "5598946926651@s.whatsapp.net" {
		t.Fatalf("destinationJID = %v, %v", jid, err)
	}
	hook.Conversation.Meta.Sender.PhoneNumber = ""
	if _, err := destinationJID(&hook); err == nil {
		t.Error("expected error without phone")
	}
}

func TestWhatsappMediaType(t *testing.T) {
	cases := []struct {
		fileType, mime, want string
	}{
		{"image", "image/png", "image"},
		{"image", "image/gif", "document"},
		{"video", "video/mp4", "video"},
		{"video", "video/quicktime", "document"},
		{"audio", "audio/webm", "audio"},
		{"file", "application/pdf", "document"},
	}
	for _, c := range cases {
		if got := whatsappMediaType(c.fileType, c.mime, nil); got != c.want {
			t.Errorf("whatsappMediaType(%q, %q) = %q, want %q", c.fileType, c.mime, got, c.want)
		}
	}
}
