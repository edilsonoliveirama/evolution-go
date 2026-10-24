package chatwoot_service

import (
	"fmt"
	"regexp"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"github.com/evolution-foundation/evolution-go/pkg/internal/waformat"
)

const sourceIDPrefix = "WAID:"

// isSupportedChat reports whether a chat should be mirrored to Chatwoot.
// Groups, status/broadcast and newsletters are never mirrored.
func isSupportedChat(jid types.JID) bool {
	switch jid.Server {
	case types.DefaultUserServer, types.HiddenUserServer:
		return jid.User != ""
	default:
		return false
	}
}

// phoneFromJID returns the E.164 phone ("+5511...") for a phone-number JID, or
// "" for LID/unknown JIDs.
func phoneFromJID(jid types.JID) string {
	if jid.Server != types.DefaultUserServer || jid.User == "" {
		return ""
	}
	return "+" + jid.User
}

// brazilVariants returns the phone with and without the mobile 9th digit for
// Brazilian numbers, so contacts created either way are matched. For any
// other number it returns only the phone itself.
func brazilVariants(phone string) []string {
	digits := strings.TrimPrefix(phone, "+")
	if !strings.HasPrefix(digits, "55") {
		return []string{phone}
	}
	switch len(digits) {
	case 13: // 55 + DDD + 9XXXXXXXX
		if digits[4] == '9' {
			return []string{"+" + digits, "+" + digits[:4] + digits[5:]}
		}
	case 12: // 55 + DDD + XXXXXXXX
		return []string{"+" + digits, "+" + digits[:4] + "9" + digits[4:]}
	}
	return []string{"+" + digits}
}

// markdownToWhatsApp converts the Markdown produced by the Chatwoot editor to
// WhatsApp formatting.
func markdownToWhatsApp(s string) string {
	return waformat.FromMarkdown(s)
}

// signMessage prefixes the agent name the same way the Evolution API does.
func signMessage(content, agentName, delimiter string) string {
	if agentName == "" {
		return content
	}
	delimiter = strings.ReplaceAll(delimiter, `\n`, "\n")
	if delimiter == "" {
		delimiter = "\n"
	}
	if content == "" {
		return fmt.Sprintf("*%s:*", agentName)
	}
	return fmt.Sprintf("*%s:*%s%s", agentName, delimiter, content)
}

// mediaInfo describes a WhatsApp message that carries a downloadable file.
type mediaInfo struct {
	Downloadable whatsmeow.DownloadableMessage
	FileName     string
	MimeType     string
	Caption      string
}

// extractMedia returns the downloadable media of a message, if any.
func extractMedia(msg *waE2E.Message, id string) *mediaInfo {
	if msg == nil {
		return nil
	}
	if img := msg.GetImageMessage(); img != nil {
		return &mediaInfo{img, id + ".jpg", orDefault(img.GetMimetype(), "image/jpeg"), img.GetCaption()}
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return &mediaInfo{vid, id + ".mp4", orDefault(vid.GetMimetype(), "video/mp4"), vid.GetCaption()}
	}
	if vid := msg.GetPtvMessage(); vid != nil {
		return &mediaInfo{vid, id + ".mp4", orDefault(vid.GetMimetype(), "video/mp4"), vid.GetCaption()}
	}
	if aud := msg.GetAudioMessage(); aud != nil {
		return &mediaInfo{aud, id + ".ogg", baseMime(orDefault(aud.GetMimetype(), "audio/ogg")), ""}
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		name := doc.GetFileName()
		if name == "" {
			name = id + extensionForMime(doc.GetMimetype())
		}
		return &mediaInfo{doc, name, orDefault(doc.GetMimetype(), "application/octet-stream"), doc.GetCaption()}
	}
	if st := msg.GetStickerMessage(); st != nil {
		return &mediaInfo{st, id + ".webp", orDefault(st.GetMimetype(), "image/webp"), ""}
	}
	return nil
}

// textContent renders the textual part of a WhatsApp message for Chatwoot.
// Returns "" when the message type has no textual representation.
func textContent(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	switch {
	case msg.GetConversation() != "":
		return msg.GetConversation()
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetText()
	case msg.GetLocationMessage() != nil:
		loc := msg.GetLocationMessage()
		return formatLocation(loc.GetName(), loc.GetAddress(), loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
	case msg.GetLiveLocationMessage() != nil:
		loc := msg.GetLiveLocationMessage()
		return formatLocation("Localização em tempo real", "", loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
	case msg.GetContactMessage() != nil:
		c := msg.GetContactMessage()
		return formatContact(c.GetDisplayName(), c.GetVcard())
	case msg.GetContactsArrayMessage() != nil:
		var parts []string
		for _, c := range msg.GetContactsArrayMessage().GetContacts() {
			parts = append(parts, formatContact(c.GetDisplayName(), c.GetVcard()))
		}
		return strings.Join(parts, "\n\n")
	case msg.GetPollCreationMessage() != nil, msg.GetPollCreationMessageV2() != nil, msg.GetPollCreationMessageV3() != nil:
		poll := msg.GetPollCreationMessage()
		if poll == nil {
			poll = msg.GetPollCreationMessageV2()
		}
		if poll == nil {
			poll = msg.GetPollCreationMessageV3()
		}
		lines := []string{"📊 *Enquete:* " + poll.GetName()}
		for _, opt := range poll.GetOptions() {
			lines = append(lines, "• "+opt.GetOptionName())
		}
		return strings.Join(lines, "\n")
	case msg.GetButtonsResponseMessage() != nil:
		return msg.GetButtonsResponseMessage().GetSelectedDisplayText()
	case msg.GetListResponseMessage() != nil:
		return msg.GetListResponseMessage().GetTitle()
	case msg.GetTemplateButtonReplyMessage() != nil:
		return msg.GetTemplateButtonReplyMessage().GetSelectedDisplayText()
	case msg.GetInteractiveResponseMessage() != nil:
		return msg.GetInteractiveResponseMessage().GetBody().GetText()
	case msg.GetButtonsMessage() != nil:
		return msg.GetButtonsMessage().GetContentText()
	case msg.GetListMessage() != nil:
		return msg.GetListMessage().GetDescription()
	case msg.GetInteractiveMessage() != nil:
		return msg.GetInteractiveMessage().GetBody().GetText()
	}
	return ""
}

// mediaPlaceholder is used by the history import, which stores text only.
func mediaPlaceholder(msg *waE2E.Message) string {
	switch {
	case msg.GetImageMessage() != nil:
		return "🖼️ _Imagem_"
	case msg.GetVideoMessage() != nil, msg.GetPtvMessage() != nil:
		return "🎥 _Vídeo_"
	case msg.GetAudioMessage() != nil:
		return "🎵 _Áudio_"
	case msg.GetDocumentMessage() != nil:
		return "📄 _Documento: " + msg.GetDocumentMessage().GetFileName() + "_"
	case msg.GetStickerMessage() != nil:
		return "_Figurinha_"
	}
	return ""
}

// contextInfo returns the ContextInfo of the message, used to find quotes.
func contextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	if msg == nil {
		return nil
	}
	switch {
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetContextInfo()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetContextInfo()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetContextInfo()
	case msg.GetPtvMessage() != nil:
		return msg.GetPtvMessage().GetContextInfo()
	case msg.GetAudioMessage() != nil:
		return msg.GetAudioMessage().GetContextInfo()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetContextInfo()
	case msg.GetStickerMessage() != nil:
		return msg.GetStickerMessage().GetContextInfo()
	case msg.GetLocationMessage() != nil:
		return msg.GetLocationMessage().GetContextInfo()
	case msg.GetContactMessage() != nil:
		return msg.GetContactMessage().GetContextInfo()
	}
	return nil
}

func formatLocation(name, address string, lat, lng float64) string {
	lines := []string{"📍 *Localização*"}
	if name != "" {
		lines = append(lines, name)
	}
	if address != "" {
		lines = append(lines, address)
	}
	lines = append(lines, fmt.Sprintf("https://www.google.com/maps/search/?api=1&query=%f,%f", lat, lng))
	return strings.Join(lines, "\n")
}

var reVcardTel = regexp.MustCompile(`(?m)^TEL[^:]*:(.+)$`)

func formatContact(name, vcard string) string {
	lines := []string{"👤 *Contato:* " + name}
	for _, m := range reVcardTel.FindAllStringSubmatch(vcard, -1) {
		lines = append(lines, "📞 "+strings.TrimSpace(m[1]))
	}
	return strings.Join(lines, "\n")
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func baseMime(m string) string {
	if i := strings.Index(m, ";"); i >= 0 {
		return strings.TrimSpace(m[:i])
	}
	return m
}

func extensionForMime(m string) string {
	switch baseMime(m) {
	case "application/pdf":
		return ".pdf"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "video/mp4":
		return ".mp4"
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "text/plain":
		return ".txt"
	case "application/zip":
		return ".zip"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "application/msword":
		return ".doc"
	case "application/vnd.ms-excel":
		return ".xls"
	}
	return ""
}

func parseIgnoreList(s string) map[string]bool {
	out := map[string]bool{}
	for _, item := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == ';' || r == ' ' }) {
		item = strings.TrimSpace(strings.TrimPrefix(item, "+"))
		if item != "" {
			out[item] = true
		}
	}
	return out
}

func isIgnored(ignore map[string]bool, jid types.JID) bool {
	if len(ignore) == 0 {
		return false
	}
	return ignore[jid.User] || ignore[jid.String()]
}
