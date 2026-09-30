package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestAlertHandlingAPI(t *testing.T) {
	env := newTestEnv(t)
	admin := env.login("admin", env.adminPW)
	body := map[string]any{"name": "demo", "provider": "aliyun", "access_key_id": "LTAI5tDEMOSHOP000002", "access_key_secret": "demo", "regions": []string{"cn-hangzhou"}}
	code, account, raw := env.do("POST", "/api/v1/accounts", admin, body)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, raw)
	}
	env.syncer.Wait()
	if code, _, raw := env.do("GET", "/api/v1/accounts/"+itoa(int(account["id"].(float64)))+"/sync-health", admin, nil); code != 200 || !strings.Contains(raw, "last_success_at") {
		t.Fatalf("health: %d %s", code, raw)
	}
	code, list, raw := env.do("GET", "/api/v1/alert-events?status=firing", admin, nil)
	if code != 200 || list["total"].(float64) == 0 {
		t.Fatalf("events: %d %s", code, raw)
	}
	event := list["items"].([]any)[0].(map[string]any)
	path := "/api/v1/alert-events/" + itoa(int(event["id"].(float64)))
	if code, _, raw := env.do("PATCH", path, admin, map[string]any{"acknowledged": true, "note": "已检查", "silence_minutes": 60}); code != http.StatusNoContent {
		t.Fatalf("handle: %d %s", code, raw)
	}
	if code, _, raw := env.do("GET", "/api/v1/alert-events", admin, nil); code != 200 || !strings.Contains(raw, `"acknowledged_by":"admin"`) || !strings.Contains(raw, "已检查") {
		t.Fatalf("handling not visible: %d %s", code, raw)
	}
	if code, _, raw := env.do("POST", path+"/retry", admin, nil); code != http.StatusConflict {
		t.Fatalf("retry without delivery: %d %s", code, raw)
	}
	if code, _, raw := env.do("PATCH", path, admin, map[string]any{"silence_minutes": -1}); code != http.StatusBadRequest {
		t.Fatalf("invalid silence: %d %s", code, raw)
	}
}
