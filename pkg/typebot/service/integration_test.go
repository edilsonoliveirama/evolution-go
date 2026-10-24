package typebot_service

// Integration test against a real Typebot viewer. Skipped unless configured:
//
//	TYPEBOT_TEST_URL=https://viewer.example.com
//	TYPEBOT_TEST_BOT=<public id of a published test bot whose first input is a choice>
//
// Each run creates a result in that bot, so use a dedicated test bot.

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestIntegrationViewerFlow(t *testing.T) {
	baseURL, bot := os.Getenv("TYPEBOT_TEST_URL"), os.Getenv("TYPEBOT_TEST_BOT")
	if baseURL == "" || bot == "" {
		t.Skip("TYPEBOT_TEST_URL/TYPEBOT_TEST_BOT not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := newViewerClient(baseURL)

	if err := c.CheckBot(ctx, bot); err != nil {
		t.Fatalf("CheckBot: %v", err)
	}
	if err := c.CheckBot(ctx, bot+"-nao-existe"); err == nil {
		t.Error("CheckBot should fail for a missing bot")
	}

	resp, err := c.StartChat(ctx, bot, map[string]string{"remoteJid": "5511900000000@s.whatsapp.net", "pushName": "Teste", "phone": "5511900000000", "instanceName": "teste"})
	if err != nil {
		t.Fatalf("StartChat: %v", err)
	}
	if resp.SessionID == "" || !isChoice(resp.Input) || len(resp.Input.Raw) == 0 {
		t.Fatalf("unexpected start: %+v", resp)
	}
	t.Logf("first reply: %+v", renderResponse(resp))

	// Answer like a WhatsApp user would: by the option number.
	answer, ok := matchChoice(resp.Input, "1")
	if !ok {
		t.Fatal("option 1 did not match")
	}
	next, err := c.ContinueChat(ctx, resp.SessionID, answer)
	if err != nil {
		t.Fatalf("ContinueChat: %v", err)
	}
	if next.Input != nil && next.Input.ID == resp.Input.ID {
		t.Fatalf("bot did not accept the option: %+v", renderResponse(next))
	}
	t.Logf("after option 1: %+v", renderResponse(next))

	if _, err := c.ContinueChat(ctx, "sessao-inexistente-123", "oi"); err != ErrSessionNotFound {
		t.Errorf("missing session error = %v", err)
	}
}
