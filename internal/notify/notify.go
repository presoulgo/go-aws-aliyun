// Package notify sends alert messages to Feishu bots and generic webhooks.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// Events carried by a message.
const (
	EventFiring   = "firing"
	EventResolved = "resolved"
	EventTest     = "test"
)

// Item is one alerting target listed in a message.
type Item struct {
	Name       string `json:"name"`
	ResourceID string `json:"resource_id,omitempty"`
	Region     string `json:"region,omitempty"`
	Value      string `json:"value"`
}

// Message is one aggregated notification: the new (or resolved) alerts of a
// rule for one account.
type Message struct {
	Event    string    `json:"event"`
	Rule     string    `json:"rule"`
	RuleName string    `json:"rule_name"`
	Title    string    `json:"title"`
	Account  string    `json:"account"`
	Provider string    `json:"provider"`
	Items    []Item    `json:"items"`
	Total    int       `json:"total"`
	At       time.Time `json:"at"`
	// Link points to the alert page when app.external_url is configured.
	Link string `json:"link,omitempty"`
}

// MaxItems is how many items a message lists; the rest are only counted.
const MaxItems = 10

// Target is a decrypted channel.
type Target struct {
	Type   string
	URL    string
	Secret string
}

var client = &http.Client{Timeout: 10 * time.Second}

// Send delivers msg to the target.
func Send(ctx context.Context, t Target, msg Message) error {
	switch t.Type {
	case model.ChannelFeishu:
		return sendFeishu(ctx, t, msg)
	case model.ChannelWebhook:
		return sendWebhook(ctx, t, msg)
	default:
		return fmt.Errorf("不支持的渠道类型 %s", t.Type)
	}
}

// SendWithRetry sends once more after a short pause when the first try fails.
func SendWithRetry(ctx context.Context, t Target, msg Message) error {
	err := Send(ctx, t, msg)
	if err == nil {
		return nil
	}
	select {
	case <-time.After(3 * time.Second):
	case <-ctx.Done():
		return err
	}
	return Send(ctx, t, msg)
}

func postJSON(ctx context.Context, url string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("地址无效：%w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败：%w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, fmt.Errorf("HTTP %d：%s", resp.StatusCode, truncate(string(data), 200))
	}
	return data, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
