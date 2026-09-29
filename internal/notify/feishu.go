package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// feishuSign implements the custom bot signature: HMAC-SHA256 keyed with
// "timestamp\nsecret" over an empty message, base64 encoded.
func feishuSign(timestamp int64, secret string) string {
	h := hmac.New(sha256.New, []byte(strconv.FormatInt(timestamp, 10)+"\n"+secret))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func sendFeishu(ctx context.Context, t Target, msg Message) error {
	body := map[string]any{"msg_type": "interactive", "card": feishuCard(msg)}
	if t.Secret != "" {
		ts := msg.At.Unix()
		body["timestamp"] = strconv.FormatInt(ts, 10)
		body["sign"] = feishuSign(ts, t.Secret)
	}
	data, err := postJSON(ctx, t.URL, body)
	if err != nil {
		return err
	}
	// The bot answers HTTP 200 with a non-zero code on errors such as a bad
	// signature or a keyword mismatch.
	var res struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(data, &res) == nil && res.Code != nil && *res.Code != 0 {
		return fmt.Errorf("飞书返回错误 %d：%s", *res.Code, res.Msg)
	}
	return nil
}

// feishuCard builds an interactive card. The item list is plain text so
// resource names are never read as markup.
func feishuCard(msg Message) map[string]any {
	color := "red"
	switch msg.Event {
	case EventResolved:
		color = "green"
	case EventTest:
		color = "blue"
	}
	var b strings.Builder
	if msg.Account != "" {
		fmt.Fprintf(&b, "账号：%s\n", msg.Account)
	}
	for _, it := range msg.Items {
		b.WriteString("· ")
		b.WriteString(it.Name)
		if it.Region != "" {
			fmt.Fprintf(&b, "（%s）", it.Region)
		}
		if it.Value != "" {
			b.WriteString("：")
			b.WriteString(it.Value)
		}
		b.WriteString("\n")
	}
	if msg.Total > len(msg.Items) {
		fmt.Fprintf(&b, "…共 %d 项\n", msg.Total)
	}
	elements := []any{
		map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": strings.TrimSpace(b.String())}},
	}
	if msg.Link != "" {
		elements = append(elements, map[string]any{
			"tag": "action",
			"actions": []any{map[string]any{
				"tag": "button", "type": "primary", "url": msg.Link,
				"text": map[string]any{"tag": "plain_text", "content": "在云枢中查看"},
			}},
		})
	}
	elements = append(elements, map[string]any{
		"tag":      "note",
		"elements": []any{map[string]any{"tag": "plain_text", "content": msg.At.Local().Format("2006-01-02 15:04:05")}},
	})
	return map[string]any{
		"config":   map[string]any{"wide_screen_mode": true},
		"header":   map[string]any{"template": color, "title": map[string]any{"tag": "plain_text", "content": msg.Title}},
		"elements": elements,
	}
}
