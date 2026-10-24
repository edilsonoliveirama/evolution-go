package typebot_service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	typebot_model "github.com/evolution-foundation/evolution-go/pkg/typebot/model"
)

// Shape returned by Typebot 3.19 (bot schema 6.1) with textBubbleContentFormat=markdown.
const sampleResponse = `{
 "sessionId": "s1",
 "messages": [
  {"id": "b1", "type": "text", "content": {"type": "markdown", "markdown": "Olá! Seja **bem-vindo(a)**\\! Veja [nosso site](https://festou.example)"}},
  {"id": "b2", "type": "image", "content": {"url": "https://cdn.example/foto.png"}},
  {"id": "b3", "type": "video", "content": {"url": "https://youtu.be/abc", "type": "youtube"}},
  {"id": "b4", "type": "text", "content": {"type": "markdown", "markdown": "Escolha uma opção:"}}
 ],
 "clientSideActions": [{"type": "wait", "lastBubbleBlockId": "b2", "wait": {"secondsToWaitFor": 2}}],
 "input": {"id": "in1", "type": "choice input", "items": [
  {"id": "item_a", "content": "Quero fazer uma reserva!"},
  {"id": "item_b", "content": "Falar com atendente"}
 ]}
}`

func parseSample(t *testing.T) *tbResponse {
	t.Helper()
	var resp tbResponse
	if err := json.Unmarshal([]byte(sampleResponse), &resp); err != nil {
		t.Fatal(err)
	}
	return &resp
}

func TestRenderResponse(t *testing.T) {
	items := renderResponse(parseSample(t))
	if len(items) != 4 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	if items[0].Kind != "text" || items[0].Text != "Olá! Seja *bem-vindo(a)*! Veja nosso site: https://festou.example" {
		t.Errorf("text = %q", items[0].Text)
	}
	if items[1].Kind != "image" || items[1].WaitAfter != 2*time.Second {
		t.Errorf("image = %+v", items[1])
	}
	if items[2].Kind != "text" || items[2].Text != "https://youtu.be/abc" {
		t.Errorf("youtube video should be a link: %+v", items[2])
	}
	want := "Escolha uma opção:\n\n*1.* Quero fazer uma reserva!\n*2.* Falar com atendente"
	if items[3].Text != want {
		t.Errorf("options = %q, want %q", items[3].Text, want)
	}
}

func TestMatchChoice(t *testing.T) {
	input := parseSample(t).Input
	cases := map[string]string{
		"1":                        "item_a",
		" 2 ":                      "item_b",
		"2.":                       "item_b",
		"1)":                       "item_a",
		"quero fazer uma reserva":  "item_a",
		"QUERO FAZER UMA RESERVA!": "item_a",
		"falar com atendente":      "item_b",
		"item_b":                   "item_b",
	}
	for in, want := range cases {
		got, ok := matchChoice(input, in)
		if !ok || got != want {
			t.Errorf("matchChoice(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"3", "0", "talvez", ""} {
		if _, ok := matchChoice(input, bad); ok {
			t.Errorf("matchChoice(%q) should not match", bad)
		}
	}
	// Free-text inputs pass the reply through.
	if got, ok := matchChoice(&tbInput{Type: "text input"}, "Maria"); !ok || got != "Maria" {
		t.Errorf("text input = %q, %v", got, ok)
	}
}

func TestMatchTrigger(t *testing.T) {
	cfg := &typebot_model.TypebotConfig{TriggerType: typebot_model.TriggerKeyword, TriggerKeywords: "festa, Reserva", TriggerOperator: typebot_model.OperatorEquals}
	if !matchTrigger(cfg, "Festa") || !matchTrigger(cfg, "reserva!") || matchTrigger(cfg, "quero uma festa") {
		t.Error("equals operator")
	}
	cfg.TriggerOperator = typebot_model.OperatorContains
	if !matchTrigger(cfg, "quero uma festa") || matchTrigger(cfg, "oi") {
		t.Error("contains operator")
	}
	cfg.TriggerOperator = typebot_model.OperatorStartsWith
	if !matchTrigger(cfg, "Reserva para sábado") || matchTrigger(cfg, "minha reserva") {
		t.Error("startsWith operator")
	}
	if !matchTrigger(&typebot_model.TypebotConfig{TriggerType: typebot_model.TriggerAll}, "qualquer") {
		t.Error("all trigger")
	}
	if matchTrigger(&typebot_model.TypebotConfig{TriggerType: typebot_model.TriggerNone}, "festa") {
		t.Error("none trigger")
	}
}

func TestKeywordFinish(t *testing.T) {
	if !isKeyword(" #SAIR ", "#sair") || isKeyword("sair", "#sair") || isKeyword("x", "") {
		t.Error("isKeyword")
	}
}

func TestEndOfFlowHasNoOptions(t *testing.T) {
	resp := &tbResponse{Messages: []tbMessage{{ID: "x", Type: "text", Content: json.RawMessage(`{"type":"markdown","markdown":"Tchau"}`)}}}
	items := renderResponse(resp)
	if len(items) != 1 || strings.Contains(items[0].Text, "*1.*") {
		t.Errorf("items = %+v", items)
	}
}
