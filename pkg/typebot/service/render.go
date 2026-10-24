package typebot_service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"github.com/evolution-foundation/evolution-go/pkg/internal/waformat"
	typebot_model "github.com/evolution-foundation/evolution-go/pkg/typebot/model"
)

// outItem is one WhatsApp message to send for a Typebot reply.
type outItem struct {
	Kind      string // text | image | video | audio
	Text      string
	URL       string
	WaitAfter time.Duration
}

const maxWait = 60 * time.Second

var reMarkdownLink = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)

// renderResponse turns the bubbles, waits and pending input of a Typebot
// response into the WhatsApp messages to send, in order.
func renderResponse(resp *tbResponse) []outItem {
	var items []outItem
	byBlock := map[string]int{} // bubble id -> index in items

	for _, m := range resp.Messages {
		var c struct {
			Type     string `json:"type"`
			Markdown string `json:"markdown"`
			URL      string `json:"url"`
		}
		_ = json.Unmarshal(m.Content, &c)

		switch m.Type {
		case "text":
			if strings.TrimSpace(c.Markdown) == "" {
				continue
			}
			items = append(items, outItem{Kind: "text", Text: formatText(c.Markdown)})
		case "image":
			if c.URL == "" {
				continue
			}
			items = append(items, outItem{Kind: "image", URL: c.URL})
		case "video":
			if c.URL == "" {
				continue
			}
			// Only direct files can be sent as video; YouTube/Vimeo/etc. go as links.
			if (c.Type == "" || c.Type == "url") && looksLikeVideoFile(c.URL) {
				items = append(items, outItem{Kind: "video", URL: c.URL})
			} else {
				items = append(items, outItem{Kind: "text", Text: c.URL})
			}
		case "audio":
			if c.URL == "" {
				continue
			}
			items = append(items, outItem{Kind: "audio", URL: c.URL})
		case "embed":
			if c.URL != "" {
				items = append(items, outItem{Kind: "text", Text: c.URL})
			}
		default:
			continue
		}
		byBlock[m.ID] = len(items) - 1
	}

	for _, a := range resp.ClientSideActions {
		if a.Type != "wait" || a.Wait == nil || a.Wait.SecondsToWaitFor <= 0 {
			continue
		}
		wait := time.Duration(a.Wait.SecondsToWaitFor * float64(time.Second))
		if wait > maxWait {
			wait = maxWait
		}
		idx, ok := byBlock[a.LastBubbleBlockID]
		if !ok {
			idx = len(items) - 1
		}
		if idx >= 0 && idx < len(items) {
			items[idx].WaitAfter += wait
		}
	}

	if options := formatChoices(resp.Input); options != "" {
		// Show the options together with the question when it is the last bubble.
		if n := len(items); n > 0 && items[n-1].Kind == "text" && items[n-1].WaitAfter == 0 {
			items[n-1].Text += "\n\n" + options
		} else {
			items = append(items, outItem{Kind: "text", Text: options})
		}
	}
	return items
}

func formatText(markdown string) string {
	s := waformat.FromMarkdown(markdown)
	s = reMarkdownLink.ReplaceAllStringFunc(s, func(m string) string {
		parts := reMarkdownLink.FindStringSubmatch(m)
		if parts[1] == parts[2] {
			return parts[2]
		}
		return parts[1] + ": " + parts[2]
	})
	// The markdown serializer escapes punctuation (e.g. "\!"), which WhatsApp shows literally.
	return strings.NewReplacer(`\!`, "!", `\.`, ".", `\-`, "-", `\(`, "(", `\)`, ")", `\#`, "#", `\+`, "+", `\_`, "_", `\*`, "*").Replace(s)
}

func looksLikeVideoFile(u string) bool {
	l := strings.ToLower(strings.SplitN(u, "?", 2)[0])
	return strings.HasSuffix(l, ".mp4")
}

func isChoice(input *tbInput) bool {
	return input != nil && (input.Type == "choice input" || input.Type == "picture choice input") && len(input.Items) > 0
}

func itemLabel(it tbChoiceItem) string {
	if it.Content != "" {
		return it.Content
	}
	return it.Title
}

// formatChoices renders the options of a choice input as a numbered list.
func formatChoices(input *tbInput) string {
	if !isChoice(input) {
		return ""
	}
	lines := make([]string, 0, len(input.Items))
	for i, it := range input.Items {
		lines = append(lines, fmt.Sprintf("*%d.* %s", i+1, itemLabel(it)))
	}
	return strings.Join(lines, "\n")
}

var accentReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// normalize lowercases, strips accents and surrounding punctuation/spaces.
func normalize(s string) string {
	s = accentReplacer.Replace(strings.ToLower(strings.TrimSpace(s)))
	return strings.Trim(s, " \t\n.!?,;:*_~\"'")
}

var reOptionNumber = regexp.MustCompile(`^\*?(\d{1,2})\*?\s*[.)\-]?$`)

// matchChoice maps the contact reply to a choice item: by option number
// ("1", "1.", "1)") or by its text, ignoring case, accents and punctuation.
// Typebot itself only accepts the exact text or the item id.
func matchChoice(input *tbInput, reply string) (string, bool) {
	if !isChoice(input) {
		return reply, true
	}
	trimmed := strings.TrimSpace(reply)
	if m := reOptionNumber.FindStringSubmatch(trimmed); m != nil {
		if n, _ := strconv.Atoi(m[1]); n >= 1 && n <= len(input.Items) {
			return input.Items[n-1].ID, true
		}
	}
	want := normalize(reply)
	for _, it := range input.Items {
		if it.ID == trimmed || normalize(itemLabel(it)) == want {
			return it.ID, true
		}
	}
	return "", false
}

// matchTrigger reports whether a message starts the bot for a contact
// without an active session.
func matchTrigger(cfg *typebot_model.TypebotConfig, text string) bool {
	switch cfg.TriggerType {
	case typebot_model.TriggerAll:
		return true
	case typebot_model.TriggerKeyword:
		msg := normalize(text)
		if msg == "" {
			return false
		}
		for _, kw := range strings.FieldsFunc(cfg.TriggerKeywords, func(r rune) bool { return r == ',' || r == '\n' || r == ';' }) {
			kw = normalize(kw)
			if kw == "" {
				continue
			}
			switch cfg.TriggerOperator {
			case typebot_model.OperatorContains:
				if strings.Contains(msg, kw) {
					return true
				}
			case typebot_model.OperatorStartsWith:
				if strings.HasPrefix(msg, kw) {
					return true
				}
			default:
				if msg == kw {
					return true
				}
			}
		}
	}
	return false
}

// incomingText extracts what the contact typed or selected.
func incomingText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	switch {
	case msg.GetConversation() != "":
		return msg.GetConversation()
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetText()
	case msg.GetButtonsResponseMessage() != nil:
		return msg.GetButtonsResponseMessage().GetSelectedDisplayText()
	case msg.GetListResponseMessage() != nil:
		return msg.GetListResponseMessage().GetTitle()
	case msg.GetTemplateButtonReplyMessage() != nil:
		return msg.GetTemplateButtonReplyMessage().GetSelectedDisplayText()
	case msg.GetInteractiveResponseMessage() != nil:
		return msg.GetInteractiveResponseMessage().GetBody().GetText()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetCaption()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetCaption()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetCaption()
	}
	return ""
}

func isKeyword(text, keyword string) bool {
	return keyword != "" && normalize(text) == normalize(keyword)
}
