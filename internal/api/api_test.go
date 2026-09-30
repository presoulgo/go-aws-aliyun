package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/auth"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud/demo"
	"github.com/presoulgo/go-aws-aliyun/internal/secret"
	"github.com/presoulgo/go-aws-aliyun/internal/service"
	"github.com/presoulgo/go-aws-aliyun/internal/store"
)

type testEnv struct {
	t       *testing.T
	handler http.Handler
	adminPW string
	syncer  *service.SyncService
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close(db) })
	audit := service.NewAuditService(db)
	users := service.NewUserService(db, audit,
		auth.NewTokens([]byte("0123456789abcdef0123456789abcdef"), time.Hour),
		auth.NewLimiter(5, 5*time.Minute), 5*time.Minute)
	_, pw, err := users.EnsureAdmin("")
	if err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte("console.log(1)"))
	_ = zw.Close()
	web := fstest.MapFS{
		"index.html":       {Data: []byte("<html>app</html>")},
		"assets/app.js":    {Data: []byte("console.log(1)")},
		"assets/app.js.gz": {Data: gz.Bytes()},
	}
	box, _ := secret.NewBox(bytes.Repeat([]byte{3}, 32))
	reg := cloud.NewRegistry(demo.NewAWS().WithoutDelay(), demo.NewAliyun().WithoutDelay())
	accounts := service.NewAccountService(db, box, reg, audit)
	syncer := service.NewSyncService(db, accounts, audit, service.SyncOptions{Concurrency: 4, TaskTimeout: 5 * time.Second})
	alerts := service.NewAlertService(db, box, audit, "")
	if err := alerts.EnsureRules(); err != nil {
		t.Fatal(err)
	}
	syncer.OnFinish = alerts.OnSyncFinished
	h, err := New(Deps{
		Meta: Meta{Name: "云枢"}, Users: users, Audit: audit, Web: web,
		Accounts: accounts, Sync: syncer,
		Alerts:    alerts,
		Resources: service.NewResourceService(db, reg, 5, 30),
		Metrics:   service.NewMetricsService(db, accounts, reg, time.Minute),
		Dashboard: service.NewDashboardService(db, 5, 30),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{t: t, handler: h, adminPW: pw, syncer: syncer}
}

func (e *testEnv) do(method, path, token string, body any) (int, map[string]any, string) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	raw := rec.Body.String()
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out, raw
}

func (e *testEnv) login(username, password string) string {
	e.t.Helper()
	code, out, raw := e.do("POST", "/api/v1/auth/login", "", map[string]string{"username": username, "password": password})
	if code != http.StatusOK {
		e.t.Fatalf("login %s: %d %s", username, code, raw)
	}
	return out["token"].(string)
}

func TestAuthAndRBAC(t *testing.T) {
	env := newTestEnv(t)

	if code, _, _ := env.do("GET", "/api/v1/auth/me", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("me without token = %d", code)
	}
	if code, _, _ := env.do("POST", "/api/v1/auth/login", "", map[string]string{"username": "admin", "password": "nope"}); code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d", code)
	}
	admin := env.login("admin", env.adminPW)

	code, me, raw := env.do("GET", "/api/v1/auth/me", admin, nil)
	if code != 200 || me["username"] != "admin" || strings.Contains(raw, "password") {
		t.Fatalf("me: %d %s", code, raw)
	}

	code, _, raw = env.do("POST", "/api/v1/users", admin, map[string]string{"username": "viewer1", "password": "viewerPass1", "role": "viewer"})
	if code != http.StatusCreated || strings.Contains(raw, "password_hash") {
		t.Fatalf("create viewer: %d %s", code, raw)
	}
	viewer := env.login("viewer1", "viewerPass1")

	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/users"},
		{"POST", "/api/v1/users"},
		{"GET", "/api/v1/audit-logs"},
		{"DELETE", "/api/v1/users/1"},
		{"PATCH", "/api/v1/alert-events/1"},
		{"POST", "/api/v1/alert-events/1/retry"},
	} {
		if code, _, _ := env.do(tc.method, tc.path, viewer, map[string]string{}); code != http.StatusForbidden {
			t.Errorf("viewer %s %s = %d, want 403", tc.method, tc.path, code)
		}
	}

	code, out, _ := env.do("GET", "/api/v1/audit-logs?range=today&category=login", admin, nil)
	if code != 200 || out["total"].(float64) < 3 {
		t.Fatalf("audit logs: %d %v", code, out)
	}

	// Changing the password revokes the old token and returns a new one.
	code, out, raw = env.do("PUT", "/api/v1/auth/password", viewer, map[string]string{"old_password": "viewerPass1", "new_password": "viewerPass2"})
	if code != 200 {
		t.Fatalf("change password: %d %s", code, raw)
	}
	if code, _, _ := env.do("GET", "/api/v1/auth/me", viewer, nil); code != http.StatusUnauthorized {
		t.Fatalf("old token after password change = %d", code)
	}
	if code, _, _ := env.do("GET", "/api/v1/auth/me", out["token"].(string), nil); code != 200 {
		t.Fatalf("new token after password change = %d", code)
	}
}

func TestSPAFallbackAndAPINotFound(t *testing.T) {
	env := newTestEnv(t)
	cases := []struct {
		path   string
		status int
		body   string
	}{
		{"/", 200, "<html>app</html>"},
		{"/resources?type=vm", 200, "<html>app</html>"},
		{"/system/users", 200, "<html>app</html>"},
		{"/assets/app.js", 200, "console.log(1)"},
		{"/assets/missing.js", 404, ""},
		{"/api/v1/nope", 404, "接口不存在"},
	}
	for _, tc := range cases {
		code, _, raw := env.do("GET", tc.path, "", nil)
		if code != tc.status || !strings.Contains(raw, tc.body) {
			t.Errorf("GET %s = %d %q", tc.path, code, raw)
		}
	}
	code, meta, _ := env.do("GET", "/api/v1/meta", "", nil)
	if code != 200 || meta["name"] != "云枢" {
		t.Fatalf("meta: %d %v", code, meta)
	}
}

func TestSPAServesPrecompressedAssets(t *testing.T) {
	env := newTestEnv(t)
	get := func(accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/assets/app.js", nil)
		if accept != "" {
			req.Header.Set("Accept-Encoding", accept)
		}
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)
		return rec
	}

	rec := get("br, gzip")
	if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("gzip response: %d %v", rec.Code, rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(zr)
	if string(body) != "console.log(1)" {
		t.Fatalf("decompressed body = %q", body)
	}

	for _, accept := range []string{"", "br", "gzip;q=0"} {
		rec = get(accept)
		if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "console.log(1)" {
			t.Errorf("Accept-Encoding %q: %d %v %q", accept, rec.Code, rec.Header(), rec.Body.String())
		}
		if rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("Accept-Encoding %q: missing Vary header", accept)
		}
	}
}

func TestAccountsSyncAndResources(t *testing.T) {
	env := newTestEnv(t)
	admin := env.login("admin", env.adminPW)
	env.do("POST", "/api/v1/users", admin, map[string]string{"username": "viewer1", "password": "viewerPass1", "role": "viewer"})
	viewer := env.login("viewer1", "viewerPass1")

	body := map[string]any{
		"name": "阿里云 电商业务", "provider": "aliyun", "access_key_id": "LTAI5tDEMOSHOP000002",
		"access_key_secret": "super-secret-value", "role_arn": "acs:ram::1507442290112290:role/ops-readonly",
		"regions": []string{"cn-hangzhou", "cn-shanghai"},
	}
	if code, _, _ := env.do("POST", "/api/v1/accounts", viewer, body); code != http.StatusForbidden {
		t.Fatalf("viewer create account = %d", code)
	}
	code, out, raw := env.do("POST", "/api/v1/accounts/test", admin, body)
	if code != 200 || out["account_uid"] != "1507442290112290" || len(out["regions"].([]any)) == 0 {
		t.Fatalf("test connection: %d %s", code, raw)
	}
	code, out, raw = env.do("POST", "/api/v1/accounts", admin, body)
	if code != http.StatusCreated || strings.Contains(raw, "super-secret-value") || strings.Contains(raw, "secret_enc") {
		t.Fatalf("create account: %d %s", code, raw)
	}
	id := int(out["id"].(float64))
	if out["access_key_masked"] != "LTAI••••••••0002" {
		t.Fatalf("masked key = %v", out["access_key_masked"])
	}
	env.syncer.Wait()

	code, _, raw = env.do("GET", "/api/v1/accounts", viewer, nil)
	if code != 200 || strings.Contains(raw, "super-secret-value") || !strings.Contains(raw, `"last_sync_status":"success"`) {
		t.Fatalf("viewer list accounts: %d %s", code, raw)
	}
	code, out, _ = env.do("GET", "/api/v1/resources?type=vm&sort=cpu_1h:desc&page_size=5", viewer, nil)
	if code != 200 || out["total"].(float64) == 0 {
		t.Fatalf("resources: %d %v", code, out)
	}
	first := out["items"].([]any)[0].(map[string]any)
	if first["name"] != "prod-api-07" || first["account_name"] != "阿里云 电商业务" {
		t.Fatalf("top resource: %v", first)
	}
	rid := int(first["id"].(float64))
	code, out, raw = env.do("GET", "/api/v1/resources/"+itoa(rid)+"/metrics?range=1h&keys=cpu_util,net_in", viewer, nil)
	if code != 200 || len(out["series"].([]any)) != 2 {
		t.Fatalf("metrics: %d %s", code, raw)
	}
	if code, _, raw = env.do("GET", "/api/v1/dashboard/summary?provider=aliyun", viewer, nil); code != 200 || !strings.Contains(raw, "prod-api-07") {
		t.Fatalf("dashboard: %d %s", code, raw)
	}
	if code, out, _ = env.do("GET", "/api/v1/sync/status", viewer, nil); code != 200 || out["state"] != "ok" {
		t.Fatalf("sync status: %d %v", code, out)
	}

	// Viewers cannot trigger syncs or change accounts.
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/v1/accounts/" + itoa(id) + "/sync"},
		{"POST", "/api/v1/sync/all"},
		{"PATCH", "/api/v1/accounts/" + itoa(id)},
		{"DELETE", "/api/v1/accounts/" + itoa(id)},
	} {
		if code, _, _ := env.do(tc.method, tc.path, viewer, map[string]any{"enabled": false}); code != http.StatusForbidden {
			t.Errorf("viewer %s %s = %d", tc.method, tc.path, code)
		}
	}
	if code, _, raw = env.do("PATCH", "/api/v1/accounts/"+itoa(id), admin, map[string]any{"enabled": false}); code != 200 || !strings.Contains(raw, `"enabled":false`) {
		t.Fatalf("disable account: %d %s", code, raw)
	}
	if code, _, _ = env.do("POST", "/api/v1/accounts/"+itoa(id)+"/sync", admin, nil); code != http.StatusConflict {
		t.Fatalf("syncing a disabled account = %d", code)
	}
	if code, _, _ = env.do("DELETE", "/api/v1/accounts/"+itoa(id), admin, nil); code != http.StatusNoContent {
		t.Fatalf("delete account = %d", code)
	}
	if _, out, _ = env.do("GET", "/api/v1/resources", admin, nil); out["total"].(float64) != 0 {
		t.Fatalf("resources must be deleted with the account: %v", out["total"])
	}
	_, out, raw = env.do("GET", "/api/v1/audit-logs?category=account", admin, nil)
	if out["total"].(float64) < 3 || !strings.Contains(raw, "account_delete") {
		t.Fatalf("account audit: %s", raw)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
