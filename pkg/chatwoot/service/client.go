package chatwoot_service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

// ErrNotFound is returned when Chatwoot answers 404 (e.g. a cached conversation
// that was deleted on the Chatwoot side).
var ErrNotFound = errors.New("chatwoot: not found")

// apiClient is a minimal client for the Chatwoot application API, scoped to a
// single account and authenticated with a user/bot access token.
type apiClient struct {
	baseURL   string
	accountID int
	token     string
	http      *http.Client
}

func newAPIClient(baseURL string, accountID int, token string) *apiClient {
	return &apiClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		accountID: accountID,
		token:     token,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

type cwInbox struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	ChannelType     string `json:"channel_type"`
	InboxIdentifier string `json:"inbox_identifier"`
	WebhookURL      string `json:"webhook_url"`
}

type cwContactInbox struct {
	SourceID string `json:"source_id"`
	Inbox    struct {
		ID int `json:"id"`
	} `json:"inbox"`
}

type cwContact struct {
	ID             int              `json:"id"`
	Name           string           `json:"name"`
	PhoneNumber    string           `json:"phone_number"`
	Identifier     string           `json:"identifier"`
	Thumbnail      string           `json:"thumbnail"`
	ContactInboxes []cwContactInbox `json:"contact_inboxes"`
}

type cwConversation struct {
	ID      int    `json:"id"`
	Status  string `json:"status"`
	InboxID int    `json:"inbox_id"`
}

type cwMessage struct {
	ID int `json:"id"`
}

type cwAttachment struct {
	FileName string
	MimeType string
	Data     []byte
}

type cwCreateMessage struct {
	Content           string                 `json:"content"`
	MessageType       string                 `json:"message_type"`
	Private           bool                   `json:"private"`
	SourceID          string                 `json:"source_id,omitempty"`
	ContentAttributes map[string]interface{} `json:"content_attributes,omitempty"`
	Attachment        *cwAttachment          `json:"-"`
}

type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("chatwoot: HTTP %d: %s", e.Status, e.Body)
}

func (c *apiClient) accountPath(format string, args ...interface{}) string {
	return fmt.Sprintf("%s/api/v1/accounts/%d", c.baseURL, c.accountID) + fmt.Sprintf(format, args...)
}

func (c *apiClient) doJSON(ctx context.Context, method, fullURL string, body interface{}, out interface{}) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

func (c *apiClient) send(req *http.Request, out interface{}) error {
	req.Header.Set("api_access_token", c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyStr := string(respBody)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500]
		}
		return &apiError{Status: resp.StatusCode, Body: bodyStr}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("chatwoot: decode response: %w", err)
		}
	}
	return nil
}

// ---- Inboxes ----

func (c *apiClient) ListInboxes(ctx context.Context) ([]cwInbox, error) {
	var out struct {
		Payload []cwInbox `json:"payload"`
	}
	err := c.doJSON(ctx, http.MethodGet, c.accountPath("/inboxes"), nil, &out)
	return out.Payload, err
}

func (c *apiClient) CreateInbox(ctx context.Context, name, webhookURL string) (*cwInbox, error) {
	body := map[string]interface{}{
		"name": name,
		"channel": map[string]interface{}{
			"type":        "api",
			"webhook_url": webhookURL,
		},
	}
	var out cwInbox
	if err := c.doJSON(ctx, http.MethodPost, c.accountPath("/inboxes"), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *apiClient) UpdateInboxWebhook(ctx context.Context, inboxID int, webhookURL string) error {
	body := map[string]interface{}{
		"channel": map[string]interface{}{
			"webhook_url": webhookURL,
		},
	}
	return c.doJSON(ctx, http.MethodPatch, c.accountPath("/inboxes/%d", inboxID), body, nil)
}

// ---- Contacts ----

func (c *apiClient) SearchContacts(ctx context.Context, query string) ([]cwContact, error) {
	var out struct {
		Payload []cwContact `json:"payload"`
	}
	u := c.accountPath("/contacts/search") + "?include_contact_inboxes=true&q=" + url.QueryEscape(query)
	err := c.doJSON(ctx, http.MethodGet, u, nil, &out)
	return out.Payload, err
}

func (c *apiClient) CreateContact(ctx context.Context, inboxID int, name, phone, identifier, avatarURL string) (*cwContact, string, error) {
	body := map[string]interface{}{
		"inbox_id":   inboxID,
		"name":       name,
		"identifier": identifier,
	}
	if phone != "" {
		body["phone_number"] = phone
	}
	if avatarURL != "" {
		body["avatar_url"] = avatarURL
	}

	var out struct {
		Payload struct {
			Contact      cwContact `json:"contact"`
			ContactInbox struct {
				SourceID string `json:"source_id"`
			} `json:"contact_inbox"`
		} `json:"payload"`
	}
	if err := c.doJSON(ctx, http.MethodPost, c.accountPath("/contacts"), body, &out); err != nil {
		return nil, "", err
	}
	return &out.Payload.Contact, out.Payload.ContactInbox.SourceID, nil
}

func (c *apiClient) UpdateContact(ctx context.Context, contactID int, fields map[string]interface{}) error {
	return c.doJSON(ctx, http.MethodPut, c.accountPath("/contacts/%d", contactID), fields, nil)
}

func (c *apiClient) CreateContactInbox(ctx context.Context, contactID, inboxID int) (string, error) {
	var out struct {
		SourceID string `json:"source_id"`
	}
	body := map[string]interface{}{"inbox_id": inboxID}
	if err := c.doJSON(ctx, http.MethodPost, c.accountPath("/contacts/%d/contact_inboxes", contactID), body, &out); err != nil {
		return "", err
	}
	return out.SourceID, nil
}

func (c *apiClient) ContactConversations(ctx context.Context, contactID int) ([]cwConversation, error) {
	var out struct {
		Payload []cwConversation `json:"payload"`
	}
	err := c.doJSON(ctx, http.MethodGet, c.accountPath("/contacts/%d/conversations", contactID), nil, &out)
	return out.Payload, err
}

// ---- Conversations ----

func (c *apiClient) CreateConversation(ctx context.Context, contactID, inboxID int, sourceID, status string) (*cwConversation, error) {
	body := map[string]interface{}{
		"contact_id": contactID,
		"inbox_id":   inboxID,
	}
	if sourceID != "" {
		body["source_id"] = sourceID
	}
	if status != "" {
		body["status"] = status
	}
	var out cwConversation
	if err := c.doJSON(ctx, http.MethodPost, c.accountPath("/conversations"), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *apiClient) ToggleConversationStatus(ctx context.Context, conversationID int, status string) error {
	body := map[string]interface{}{"status": status}
	return c.doJSON(ctx, http.MethodPost, c.accountPath("/conversations/%d/toggle_status", conversationID), body, nil)
}

// ---- Messages ----

func (c *apiClient) CreateMessage(ctx context.Context, conversationID int, msg cwCreateMessage) (*cwMessage, error) {
	endpoint := c.accountPath("/conversations/%d/messages", conversationID)
	var out cwMessage

	if msg.Attachment == nil {
		if err := c.doJSON(ctx, http.MethodPost, endpoint, msg, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("content", msg.Content)
	_ = w.WriteField("message_type", msg.MessageType)
	_ = w.WriteField("private", fmt.Sprintf("%t", msg.Private))
	if msg.SourceID != "" {
		_ = w.WriteField("source_id", msg.SourceID)
	}
	for k, v := range msg.ContentAttributes {
		_ = w.WriteField(fmt.Sprintf("content_attributes[%s]", k), fmt.Sprintf("%v", v))
	}

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="attachments[]"; filename="%s"`, escapeQuotes(msg.Attachment.FileName)))
	h.Set("Content-Type", msg.Attachment.MimeType)
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(msg.Attachment.Data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if err := c.send(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *apiClient) DeleteMessage(ctx context.Context, conversationID, messageID int) error {
	return c.doJSON(ctx, http.MethodDelete, c.accountPath("/conversations/%d/messages/%d", conversationID, messageID), nil, nil)
}

// UpdateLastSeen marks the conversation as seen by the contact (shows the
// agent's messages as read). Uses the public client API of the API inbox.
func (c *apiClient) UpdateLastSeen(ctx context.Context, inboxIdentifier, contactSourceID string, conversationID int) error {
	u := fmt.Sprintf("%s/public/api/v1/inboxes/%s/contacts/%s/conversations/%d/update_last_seen",
		c.baseURL, url.PathEscape(inboxIdentifier), url.PathEscape(contactSourceID), conversationID)
	return c.doJSON(ctx, http.MethodPost, u, map[string]interface{}{}, nil)
}

// Download fetches an attachment URL sent by Chatwoot in a webhook.
func (c *apiClient) Download(ctx context.Context, fileURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, "", err
	}
	// Attachment URLs are signed ActiveStorage links; the token only matters
	// when they point back to the Chatwoot API itself.
	if strings.HasPrefix(fileURL, c.baseURL) {
		req.Header.Set("api_access_token", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("chatwoot: download attachment: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 100<<20))
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func escapeQuotes(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
