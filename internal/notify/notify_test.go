package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

func TestFeishuSign(t *testing.T) {
	// Reference value computed independently with Python hmac per the Feishu bot docs.
	if got := feishuSign(1599360473, "demo"); got != "l1N0gAcBjdwBvGm1xMjOF0XSyaLRpR7tuO5dHfhAYc8=" {
		t.Fatalf("sign = %s", got)
	}
}

func TestSendFeishuAndWebhook(t *testing.T) {
	var got map[string]any
	code := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": "sign match fail or timestamp is not within one hour from current time"})
	}))
	defer srv.Close()
	msg := Message{Event: EventFiring, Rule: "cpu_high", Title: "主机 CPU 过高", Account: "prod", Total: 1,
		Items: []Item{{Name: "web-01", Region: "cn-hangzhou", Value: "CPU 95%"}}, At: time.Unix(1599360473, 0)}

	if err := Send(context.Background(), Target{Type: model.ChannelFeishu, URL: srv.URL, Secret: "demo"}, msg); err != nil {
		t.Fatal(err)
	}
	if got["sign"] != "l1N0gAcBjdwBvGm1xMjOF0XSyaLRpR7tuO5dHfhAYc8=" || got["timestamp"] != "1599360473" || got["msg_type"] != "interactive" {
		t.Fatalf("feishu body: %v", got)
	}
	code = 19021
	if err := Send(context.Background(), Target{Type: model.ChannelFeishu, URL: srv.URL}, msg); err == nil {
		t.Fatal("non-zero feishu code must fail")
	}

	code = 0
	if err := Send(context.Background(), Target{Type: model.ChannelWebhook, URL: srv.URL}, msg); err != nil {
		t.Fatal(err)
	}
	items, _ := got["items"].([]any)
	if got["event"] != "firing" || got["rule"] != "cpu_high" || len(items) != 1 {
		t.Fatalf("webhook body: %v", got)
	}
}
