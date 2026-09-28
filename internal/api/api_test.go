package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud/demo"
	"github.com/presoulgo/go-aws-aliyun/internal/config"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/store"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDemoFlowAndRBAC(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	s := New(db, config.Config{Demo: true, SecretKey: "test-key", AdminPassword: "AdminPassword123", IdleCPU: 5, ExpiringDays: 30})
	if _, err = s.EnsureAdmin(); err != nil {
		t.Fatal(err)
	}
	if err = demo.Seed(db); err != nil {
		t.Fatal(err)
	}
	h, _ := bcrypt.GenerateFromPassword([]byte("ViewerPassword123"), bcrypt.DefaultCost)
	db.Create(&model.User{Username: "viewer", PasswordHash: string(h), Role: "viewer", Enabled: true})
	router := s.Router(http.NotFoundHandler())
	call := func(method, path, token string, body any) *httptest.ResponseRecorder {
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	login := func(name, password string) string {
		w := call("POST", "/api/v1/auth/login", "", map[string]string{"username": name, "password": password})
		if w.Code != 200 {
			t.Fatalf("login %s: %d %s", name, w.Code, w.Body.String())
		}
		var data struct {
			Token string `json:"token"`
		}
		json.Unmarshal(w.Body.Bytes(), &data)
		return data.Token
	}
	admin := login("admin", "AdminPassword123")
	viewer := login("viewer", "ViewerPassword123")
	w := call("GET", "/api/v1/dashboard/summary", admin, nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var summary map[string]any
	json.Unmarshal(w.Body.Bytes(), &summary)
	for k, want := range map[string]float64{"accounts": 6, "resources": 784, "regions": 27, "running_vms": 441, "idle_vms": 37, "expiring": 12} {
		if summary[k] != want {
			t.Errorf("%s=%v, want %v", k, summary[k], want)
		}
	}
	w = call("GET", "/api/v1/resources?type=vm&idle=1&q=env:prod", viewer, nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var resources map[string]any
	json.Unmarshal(w.Body.Bytes(), &resources)
	if resources["total"].(float64) == 0 {
		t.Fatal("idle tag filter returned no rows")
	}
	w = call("GET", "/api/v1/accounts", viewer, nil)
	if strings.Contains(w.Body.String(), "secret_encrypted") || strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("sensitive data leaked")
	}
	if w = call("POST", "/api/v1/sync/all", viewer, nil); w.Code != 403 {
		t.Fatalf("viewer write status=%d", w.Code)
	}
	if w = call("GET", "/api/v1/audit-logs", viewer, nil); w.Code != 403 {
		t.Fatalf("viewer audit status=%d", w.Code)
	}
	if w = call("POST", "/api/v1/auth/login", "", map[string]string{"username": "viewer", "password": "wrong-password"}); w.Code != 401 {
		t.Fatalf("bad login status=%d", w.Code)
	}
	w = call("GET", "/api/v1/users", admin, nil)
	var userList struct {
		Items []struct {
			Username          string `json:"username"`
			TodayFailedLogins int    `json:"today_failed_logins"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &userList); err != nil {
		t.Fatal(err)
	}
	foundFailure := false
	for _, item := range userList.Items {
		if item.Username == "viewer" && item.TodayFailedLogins == 1 {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatalf("today login failures missing: %s", w.Body.String())
	}
	var viewerUser model.User
	db.Where("username = ?", "viewer").First(&viewerUser)
	if w = call("POST", fmt.Sprintf("/api/v1/users/%d/reset-password", viewerUser.ID), admin, map[string]string{"password": "letters-only-password"}); w.Code != 400 {
		t.Fatalf("weak reset password status=%d", w.Code)
	}
	if w = call("POST", fmt.Sprintf("/api/v1/users/%d/reset-password", viewerUser.ID), admin, map[string]string{"password": "NewViewerPassword456"}); w.Code != 200 {
		t.Fatalf("reset password status=%d: %s", w.Code, w.Body.String())
	}
	if w = call("GET", "/api/v1/dashboard/summary", viewer, nil); w.Code != 401 {
		t.Fatalf("old token after reset status=%d", w.Code)
	}
	_ = login("viewer", "NewViewerPassword456")
	var account model.CloudAccount
	db.First(&account)
	job := s.startSync(account, "admin")
	again := s.startSync(account, "admin")
	if job.ID != again.ID {
		t.Fatal("duplicate job started for one account")
	}
	s.mu.Lock()
	cancel := s.cancels[job.ID]
	s.mu.Unlock()
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		db.First(&job, job.ID)
		if job.Status == "cancelled" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if job.Status != "cancelled" {
		t.Fatal(fmt.Sprint("cancel status=", job.Status))
	}
}
