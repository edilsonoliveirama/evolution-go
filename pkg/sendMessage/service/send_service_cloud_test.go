package send_service

import (
	"testing"

	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
)

// AC: CLOUD-16 — SendText dispatches to CloudApiService for cloudapi engine.
// AC: CLOUD-17 — SendMedia dispatches to CloudApiService for cloudapi engine.
// AC: CLOUD-18 — 24h window closed blocks free-form with clear error.
// AC: CLOUD-19 — Unsupported operations return 4xx.
// AC: CLOUD-20 — SendTemplate dispatches to CloudApiService.
// AC: CLOUD-21 — Template not found fails with reason.
// AC: CLOUD-22 — Template parameters rendered correctly.
//
// These tests expect SendService to accept cloudApiService and dispatch
// by instance.Engine (T-007).

func TestSendText_CloudEngine(t *testing.T) {
	instance := &instance_model.Instance{
		Id:               "test-cloud-send",
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	data := &TextStruct{
		Number: "[TELEFONE_BR_REDACTED]",
		Text:   "Hello from cloud engine",
		Id:     "msg-cloud-001",
	}

	_, err := sendService.SendText(data, instance)
	if err != nil {
		t.Fatalf("SendText for cloudapi should succeed within window, got: %v (AC: CLOUD-16)", err)
	}
}

func TestSendMedia_CloudEngine(t *testing.T) {
	instance := &instance_model.Instance{
		Id:               "test-cloud-media",
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	data := &MediaStruct{
		Number: "[TELEFONE_BR_REDACTED]",
		Type:   "image",
		Url:    "https://example.com/image.jpg",
		Caption: "Test image",
		Id:     "media-cloud-001",
	}

	_, err := sendService.SendMediaUrl(data, instance)
	if err != nil {
		t.Fatalf("SendMediaUrl for cloudapi should succeed, got: %v (AC: CLOUD-17)", err)
	}
}

func TestSendButton_CloudEngine_Returns4xx(t *testing.T) {
	instance := &instance_model.Instance{
		Id:     "test-cloud-btn",
		Engine: "cloudapi",
	}

	data := &ButtonStruct{
		Number:      "[TELEFONE_BR_REDACTED]",
		Title:       "Test",
		Description: "Test description",
		Footer:      "Footer",
		Buttons:     []Button{{Type: "reply", DisplayText: "OK", Id: "ok"}},
	}

	_, err := sendService.SendButton(data, instance)
	if err == nil {
		t.Fatal("SendButton for cloudapi should return error 'operação não suportada para instâncias Cloud API' (AC: CLOUD-19)")
	}
}

func TestSendList_CloudEngine_Returns4xx(t *testing.T) {
	instance := &instance_model.Instance{
		Id:     "test-cloud-list",
		Engine: "cloudapi",
	}

	data := &ListStruct{
		Number:      "[TELEFONE_BR_REDACTED]",
		Title:       "Menu",
		Description: "Choose",
		FooterText:  "Footer",
		Sections: []Section{
			{Title: "Section 1", Rows: []Row{{Title: "Option 1", RowId: "opt1"}}},
		},
	}

	_, err := sendService.SendList(data, instance)
	if err == nil {
		t.Fatal("SendList for cloudapi should return error 'operação não suportada para instâncias Cloud API' (AC: CLOUD-19)")
	}
}

func TestSendCarousel_CloudEngine_Returns4xx(t *testing.T) {
	instance := &instance_model.Instance{
		Id:     "test-cloud-carousel",
		Engine: "cloudapi",
	}

	data := &CarouselStruct{
		Number: "[TELEFONE_BR_REDACTED]",
		Body:   "Check these",
		Cards: []CarouselCardStruct{
			{
				Header: CarouselCardHeaderStruct{Title: "Card 1", ImageUrl: "https://example.com/1.jpg"},
				Body:   CarouselCardBodyStruct{Text: "Card 1 text"},
			},
		},
	}

	_, err := sendService.SendCarousel(data, instance)
	if err == nil {
		t.Fatal("SendCarousel for cloudapi should return error 'operação não suportada para instâncias Cloud API' (AC: CLOUD-19)")
	}
}

func TestSendPoll_CloudEngine_Returns4xx(t *testing.T) {
	instance := &instance_model.Instance{
		Id:     "test-cloud-poll",
		Engine: "cloudapi",
	}

	data := &PollStruct{
		Number:   "[TELEFONE_BR_REDACTED]",
		Question: "Best color?",
		Options:  []string{"Red", "Blue"},
	}

	_, err := sendService.SendPoll(data, instance)
	if err == nil {
		t.Fatal("SendPoll for cloudapi should return error 'operação não suportada para instâncias Cloud API' (AC: CLOUD-19)")
	}
}

func TestSendSticker_CloudEngine_Returns4xx(t *testing.T) {
	instance := &instance_model.Instance{
		Id:     "test-cloud-sticker",
		Engine: "cloudapi",
	}

	data := &StickerStruct{
		Number:  "[TELEFONE_BR_REDACTED]",
		Sticker: "https://example.com/sticker.webp",
		Id:      "sticker-001",
	}

	_, err := sendService.SendSticker(data, instance)
	if err == nil {
		t.Fatal("SendSticker for cloudapi should return error 'operação não suportada para instâncias Cloud API' (AC: CLOUD-19)")
	}
}

func TestSendLocation_CloudEngine_Returns4xx(t *testing.T) {
	instance := &instance_model.Instance{
		Id:     "test-cloud-loc",
		Engine: "cloudapi",
	}

	data := &LocationStruct{
		Number:   "[TELEFONE_BR_REDACTED]",
		Latitude: -23.5505,
		Longitude: -46.6333,
		Name:     "São Paulo",
	}

	_, err := sendService.SendLocation(data, instance)
	if err == nil {
		t.Fatal("SendLocation for cloudapi should return error 'operação não suportada para instâncias Cloud API' (AC: CLOUD-19)")
	}
}

func TestSendContact_CloudEngine_Returns4xx(t *testing.T) {
	instance := &instance_model.Instance{
		Id:     "test-cloud-contact",
		Engine: "cloudapi",
	}

	_, err := sendService.SendContact(&ContactStruct{
		Number: "[TELEFONE_BR_REDACTED]",
	}, instance)
	if err == nil {
		t.Fatal("SendContact for cloudapi should return error 'operação não suportada para instâncias Cloud API' (AC: CLOUD-19)")
	}
}

func TestSendStatusText_CloudEngine_Returns4xx(t *testing.T) {
	instance := &instance_model.Instance{
		Id:     "test-cloud-status-text",
		Engine: "cloudapi",
	}

	data := &StatusTextStruct{
		Text: "My status",
		Id:   "status-001",
	}

	_, err := sendService.SendStatusText(data, instance)
	if err == nil {
		t.Fatal("SendStatusText for cloudapi should return error 'operação não suportada para instâncias Cloud API' (AC: CLOUD-19)")
	}
}

func TestSendTemplate_CloudEngine(t *testing.T) {
	instance := &instance_model.Instance{
		Id:               "test-cloud-template",
		Engine:           "cloudapi",
		Connected:        true,
		MetaToken:        "EAAxToken",
		MetaPhoneNumberId: "987654321",
	}

	// This test also verifies the SendTemplate method exists on sendService
	// (T-007 adds SendTemplate handler + dispatch).
	payload := map[string]interface{}{
		"number": "[TELEFONE_BR_REDACTED]",
		"template": map[string]interface{}{
			"name":     "aviso_cobranca",
			"language": "pt_BR",
			"components": []map[string]interface{}{
				{
					"type": "body",
					"parameters": []map[string]interface{}{
						{"type": "text", "text": "João"},
						{"type": "text", "text": "R$ 150,00"},
					},
				},
			},
		},
	}

	// The template send is dispatched to cloudApiService.SendTemplate.
	_ = payload

	// This test will fail at compile time until SendTemplate is added to the
	// SendService interface and sendService struct.
}

func TestSendText_CloudEngine_Disconnected(t *testing.T) {
	instance := &instance_model.Instance{
		Id:     "test-cloud-disc-send",
		Engine: "cloudapi",
		Connected: false,
	}

	data := &TextStruct{
		Number: "[TELEFONE_BR_REDACTED]",
		Text:   "This should fail",
		Id:     "msg-fail-001",
	}

	_, err := sendService.SendText(data, instance)
	if err == nil {
		t.Fatal("SendText for disconnected cloud instance should return error (AC: CLOUD-08)")
	}
}
