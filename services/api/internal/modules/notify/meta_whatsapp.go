package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// MetaWhatsApp sends WhatsApp messages through Meta's WhatsApp Cloud API.
//
// Outside the 24-hour session window only pre-approved templates may be sent,
// so a delivery with a ProviderTemplateName goes out as a template with its
// positional ParamValues; anything else is sent as session text.
type MetaWhatsApp struct {
	AccessToken   string
	PhoneNumberID string
	// APIVersion is the Graph API version, e.g. "v21.0".
	APIVersion string
	// BaseURL defaults to https://graph.facebook.com; tests point it elsewhere.
	BaseURL string
	Client  *http.Client
}

func (MetaWhatsApp) Name() string { return "meta_whatsapp" }

type metaParam struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type metaComponent struct {
	Type       string      `json:"type"`
	Parameters []metaParam `json:"parameters"`
}

type metaTemplate struct {
	Name       string            `json:"name"`
	Language   map[string]string `json:"language"`
	Components []metaComponent   `json:"components,omitempty"`
}

type metaMessage struct {
	MessagingProduct string            `json:"messaging_product"`
	To               string            `json:"to"`
	Type             string            `json:"type"`
	Template         *metaTemplate     `json:"template,omitempty"`
	Text             map[string]string `json:"text,omitempty"`
}

func (p MetaWhatsApp) Send(ctx context.Context, d Delivery) (string, error) {
	to := strings.TrimPrefix(strings.TrimSpace(d.Destination), "+")
	if to == "" {
		return "", fmt.Errorf("%w: no destination", ErrPermanent)
	}
	msg := metaMessage{MessagingProduct: "whatsapp", To: to}
	if d.ProviderTemplateName != "" {
		lang := "en"
		if strings.HasPrefix(d.Locale, "ar") {
			lang = "ar"
		}
		t := &metaTemplate{Name: d.ProviderTemplateName, Language: map[string]string{"code": lang}}
		if len(d.ParamValues) > 0 {
			params := make([]metaParam, len(d.ParamValues))
			for i, v := range d.ParamValues {
				params[i] = metaParam{Type: "text", Text: v}
			}
			t.Components = []metaComponent{{Type: "body", Parameters: params}}
		}
		msg.Type, msg.Template = "template", t
	} else {
		msg.Type, msg.Text = "text", map[string]string{"body": d.Body}
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("%w: encode: %v", ErrPermanent, err)
	}
	base := p.BaseURL
	if base == "" {
		base = "https://graph.facebook.com"
	}
	version := p.APIVersion
	if version == "" {
		version = "v21.0"
	}
	url := fmt.Sprintf("%s/%s/%s/messages", strings.TrimRight(base, "/"), version, p.PhoneNumberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("whatsapp: send: %w", err)
	}
	defer res.Body.Close()

	var out struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)

	if res.StatusCode >= 300 {
		reason := res.Status
		if out.Error != nil {
			reason = fmt.Sprintf("%d %s", out.Error.Code, out.Error.Message)
		}
		// 429 and 5xx are worth retrying; other 4xx (bad number, unapproved
		// template, bad token) will fail the same way every time.
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
			return "", fmt.Errorf("whatsapp: %s", reason)
		}
		return "", fmt.Errorf("%w: whatsapp: %s", ErrPermanent, reason)
	}
	if len(out.Messages) == 0 || out.Messages[0].ID == "" {
		return "", fmt.Errorf("whatsapp: no message id in response")
	}
	return out.Messages[0].ID, nil
}
