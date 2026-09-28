package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/auth"
	"github.com/presoulgo/go-aws-aliyun/internal/service"
	"github.com/presoulgo/go-aws-aliyun/internal/store"
)

type testEnv struct {
	t       *testing.T
	handler http.Handler
	adminPW string
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
	web := fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	h, err := New(Deps{Meta: Meta{Name: "云枢"}, Users: users, Audit: audit, Web: web})
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{t: t, handler: h, adminPW: pw}
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
