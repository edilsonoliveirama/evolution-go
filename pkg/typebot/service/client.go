package typebot_service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrSessionNotFound is returned by continueChat when the session ended or expired.
var ErrSessionNotFound = errors.New("typebot: session not found")

// viewerClient talks to the public chat API of a Typebot viewer (v3, bot schema 6.x).
type viewerClient struct {
	baseURL string
	http    *http.Client
}

func newViewerClient(baseURL string) *viewerClient {
	return &viewerClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

type tbChoiceItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Title   string `json:"title"` // picture choice
}

type tbInput struct {
	ID    string          `json:"id"`
	Type  string          `json:"type"`
	Items []tbChoiceItem  `json:"items,omitempty"`
	Raw   json.RawMessage `json:"-"`
}

type tbMessage struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
}

type tbAction struct {
	Type              string `json:"type"`
	LastBubbleBlockID string `json:"lastBubbleBlockId"`
	Wait              *struct {
		SecondsToWaitFor float64 `json:"secondsToWaitFor"`
	} `json:"wait"`
}

type tbResponse struct {
	SessionID         string      `json:"sessionId"`
	Messages          []tbMessage `json:"messages"`
	Input             *tbInput    `json:"input"`
	ClientSideActions []tbAction  `json:"clientSideActions"`
}

type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("typebot: HTTP %d: %s", e.Status, e.Body)
}

func (c *viewerClient) post(ctx context.Context, path string, body interface{}) (*tbResponse, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrSessionNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s := string(respBody)
		if len(s) > 500 {
			s = s[:500]
		}
		return nil, &apiError{Status: resp.StatusCode, Body: s}
	}

	var out tbResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("typebot: decode response: %w", err)
	}
	// Keep the raw input so it can be stored in the session and re-read later.
	var raw struct {
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(respBody, &raw) == nil && out.Input != nil {
		out.Input.Raw = raw.Input
	}
	return &out, nil
}

func (c *viewerClient) StartChat(ctx context.Context, publicID string, prefilled map[string]string) (*tbResponse, error) {
	body := map[string]interface{}{
		"isStreamEnabled":         false,
		"textBubbleContentFormat": "markdown",
	}
	if len(prefilled) > 0 {
		body["prefilledVariables"] = prefilled
	}
	resp, err := c.post(ctx, "/api/v1/typebots/"+url.PathEscape(publicID)+"/startChat", body)
	if errors.Is(err, ErrSessionNotFound) {
		return nil, errors.New("typebot: bot not found or not published")
	}
	return resp, err
}

func (c *viewerClient) ContinueChat(ctx context.Context, sessionID, text string) (*tbResponse, error) {
	body := map[string]interface{}{
		"message":                 map[string]string{"type": "text", "text": text},
		"textBubbleContentFormat": "markdown",
	}
	return c.post(ctx, "/api/v1/sessions/"+url.PathEscape(sessionID)+"/continueChat", body)
}

// CheckBot verifies that the bot is published on the viewer without starting a
// chat (which would create a result in Typebot).
func (c *viewerClient) CheckBot(ctx context.Context, publicID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/"+url.PathEscape(publicID), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errors.New("bot não encontrado ou não publicado nesse viewer")
	case resp.StatusCode >= 400:
		return fmt.Errorf("viewer respondeu HTTP %d", resp.StatusCode)
	}
	return nil
}
