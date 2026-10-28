package send_service

import (
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestInteractiveVariants(t *testing.T) {
	nodes := []waBinary.Node{{Tag: "biz"}, {Tag: "bot"}}
	var names []string
	for _, v := range interactiveVariants(nodes) {
		names = append(names, v.name)
	}
	want := []string{"biz+bot", "biz", "no-nodes", "no-nodes-unwrapped"}
	if len(names) != len(want) {
		t.Fatalf("variants %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("variants %v, want %v", names, want)
		}
	}

	// Groups have no bot node: no duplicate attempt.
	if v := interactiveVariants([]waBinary.Node{{Tag: "biz"}}); len(v) != 3 {
		t.Fatalf("expected 3 variants without a bot node, got %d", len(v))
	}
}

func TestUnwrapDocumentWithCaption(t *testing.T) {
	secret := []byte("secret")
	msg := &waE2E.Message{
		DocumentWithCaptionMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
			ListMessage: &waE2E.ListMessage{Title: proto.String("menu")},
		}},
		MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: secret},
	}
	out := unwrapDocumentWithCaption(msg)
	if out.GetDocumentWithCaptionMessage() != nil || out.GetListMessage().GetTitle() != "menu" {
		t.Fatalf("list not moved to the top level: %v", out)
	}
	if string(out.GetMessageContextInfo().GetMessageSecret()) != "secret" {
		t.Fatal("message secret lost")
	}
	if msg.GetDocumentWithCaptionMessage().GetMessage().GetMessageContextInfo() != nil {
		t.Fatal("original message modified")
	}
}
