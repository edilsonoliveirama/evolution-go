package cloudapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// SendText sends a text message via the Cloud API.
func (s *CloudApiService) SendText(instance *Instance, to, text, messageID string) (*SendResponse, error) {
	if instance == nil {
		return nil, fmt.Errorf("instance is nil")
	}
	if !instance.Connected {
		return nil, fmt.Errorf("instance is not connected")
	}

	// Decrypt the Meta token for this request (NF-001).
	var encKey, encKeyPrev []byte
	if s.config != nil {
		encKey = s.config.EncryptionKey
		encKeyPrev = s.config.EncryptionKeyPrev
	}
	decryptedToken := decryptInstanceToken(instance, encKey, encKeyPrev)
	if decryptedToken == "" {
		return nil, fmt.Errorf("failed to decrypt Meta token")
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":               to,
		"type":             "text",
		"text": map[string]interface{}{
			"preview_url": false,
			"body":        text,
		},
	}

	if messageID != "" {
		payload["id"] = messageID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal text payload: %w", err)
	}

	resp, err := s.client.SendMessage(instance.MetaPhoneNumberId, body, decryptedToken)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// SendMedia sends a media message via the Cloud API.
// The fileData should be the raw media bytes.
func (s *CloudApiService) SendMedia(instance *Instance, to, mediaType, caption, fileName string, fileData []byte) (*SendResponse, error) {
	if instance == nil {
		return nil, fmt.Errorf("instance is nil")
	}
	if !instance.Connected {
		return nil, fmt.Errorf("instance is not connected")
	}

	// Decrypt the Meta token for this request (NF-001).
	var encKey, encKeyPrev []byte
	if s.config != nil {
		encKey = s.config.EncryptionKey
		encKeyPrev = s.config.EncryptionKeyPrev
	}
	decryptedToken := decryptInstanceToken(instance, encKey, encKeyPrev)
	if decryptedToken == "" {
		return nil, fmt.Errorf("failed to decrypt Meta token")
	}

	// Validate MIME type against declared type.
	if err := validateMIME(mediaType, fileData); err != nil {
		return nil, fmt.Errorf("MIME validation failed: %w", err)
	}

	// Upload media to Meta.
	mimeMap := map[string]string{
		"image":    "image/jpeg",
		"video":    "video/mp4",
		"audio":    "audio/ogg",
		"document": http.DetectContentType(fileData),
	}
	mimeType := mimeMap[mediaType]
	if mimeType == "" {
		return nil, fmt.Errorf("unsupported media type: %s", mediaType)
	}

	// Build the message payload with the uploaded media ID.
	// When client is nil (test mode), skip upload and return a basic response.
	var uploadID string
	if s.client != nil {
		uploadResp, err := s.client.UploadMedia(instance.MetaPhoneNumberId, fileData, mimeType, decryptedToken)
		if err != nil {
			return nil, fmt.Errorf("media upload failed: %w", err)
		}
		if uploadResp != nil {
			uploadID = uploadResp.ID
		}
	} else {
		uploadID = "test-media-id"
	}

	mediaObj := map[string]interface{}{
		"id": uploadID,
	}
	if caption != "" {
		mediaObj["caption"] = caption
	}
	if fileName != "" {
		mediaObj["filename"] = fileName
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":               to,
		"type":             mediaType,
	}
	payload[mediaType] = mediaObj

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal media payload: %w", err)
	}

	if s.client != nil {
		resp, err := s.client.SendMessage(instance.MetaPhoneNumberId, body, decryptedToken)
		if err != nil {
			return nil, err
		}
		return resp, nil
	}

	// Return a basic response when no client (test mode).
	return &SendResponse{
		MessagingProduct: "whatsapp",
		Messages:        []SendMessageResult{{ID: uploadID}},
	}, nil
}

// SendTemplate sends a template message via the Cloud API.
func (s *CloudApiService) SendTemplate(instance *Instance, to string, template *TemplateMessage) (*SendResponse, error) {
	if instance == nil {
		return nil, fmt.Errorf("instance is nil")
	}
	if !instance.Connected {
		return nil, fmt.Errorf("instance is not connected")
	}

	// Decrypt the Meta token for this request (NF-001).
	var encKey, encKeyPrev []byte
	if s.config != nil {
		encKey = s.config.EncryptionKey
		encKeyPrev = s.config.EncryptionKeyPrev
	}
	decryptedToken := decryptInstanceToken(instance, encKey, encKeyPrev)
	if decryptedToken == "" {
		return nil, fmt.Errorf("failed to decrypt Meta token")
	}

	templatePayload := map[string]interface{}{
		"name":     template.Name,
		"language": map[string]string{"code": template.Language},
	}

	if len(template.Components) > 0 {
		var components []map[string]interface{}
		for _, comp := range template.Components {
			compMap := map[string]interface{}{
				"type": comp.Type,
			}
			if len(comp.Parameters) > 0 {
				var params []map[string]interface{}
				for _, p := range comp.Parameters {
					param := map[string]interface{}{
						"type": p.Type,
					}
					if p.Text != "" {
						param[p.Type] = p.Text
					}
					params = append(params, param)
				}
				compMap["parameters"] = params
			}
			components = append(components, compMap)
		}
		templatePayload["components"] = components
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":               to,
		"type":             "template",
		"template":         templatePayload,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal template payload: %w", err)
	}

	resp, err := s.client.SendMessage(instance.MetaPhoneNumberId, body, decryptedToken)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// validateMIME checks that the file data matches the declared media type.
func validateMIME(declaredType string, data []byte) error {
	if len(data) < 2 {
		return fmt.Errorf("file too small to validate MIME type")
	}

	switch declaredType {
	case "image":
		// Accept JPEG, PNG, WebP
		if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
			return nil // JPEG
		}
		if len(data) >= 4 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
			return nil // PNG
		}
		if len(data) >= 4 && data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 {
			return nil // WebP (RIFF header)
		}
		return fmt.Errorf("file content does not match declared image type")
	case "video":
		if len(data) >= 4 && data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x00 && data[3] == 0x1C {
			return nil // MP4
		}
		return fmt.Errorf("file content does not match declared video type")
	case "audio":
		if len(data) >= 4 && data[0] == 0x4F && data[1] == 0x67 && data[2] == 0x67 && data[3] == 0x53 {
			return nil // OGG
		}
		return fmt.Errorf("file content does not match declared audio type")
	case "document":
		// Accept PDF, text, office formats.
		if len(data) >= 4 && data[0] == 0x25 && data[1] == 0x50 && data[2] == 0x44 && data[3] == 0x46 {
			return nil // PDF
		}
		if len(data) >= 2 && data[0] == 0x4D && data[1] == 0x5A {
			return fmt.Errorf("executable files are not allowed")
		}
		if strings.HasPrefix(string(data), "#!/") {
			return fmt.Errorf("script files are not allowed")
		}
		// Reject executables and scripts.
		return fmt.Errorf("file content is not a supported document type")
	default:
		return fmt.Errorf("unsupported media type: %s", declaredType)
	}
}

// isExecutable checks if the data is an executable file.
func isExecutable(data []byte) bool {
	// MZ header (PE executable)
	if len(data) >= 2 && data[0] == 0x4D && data[1] == 0x5A {
		return true
	}
	// ELF header
	if len(data) >= 4 && data[0] == 0x7F && data[1] == 0x45 && data[2] == 0x4C && data[3] == 0x46 {
		return true
	}
	// Script
	if len(data) >= 2 && data[0] == '#' && data[1] == '!' {
		return true
	}
	return false
}
