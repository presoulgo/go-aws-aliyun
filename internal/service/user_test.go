package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/presoulgo/go-aws-aliyun/internal/apperr"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

func status(err error) int {
	if e, ok := apperr.As(err); ok {
		return e.Status
	}
	return 0
}

func TestEnsureAdminAndLogin(t *testing.T) {
	db := newTestDB(t)
	users, audit := newTestUserService(t, db)

	created, pw, err := users.EnsureAdmin("")
	if err != nil || !created || len(pw) < 10 {
		t.Fatalf("EnsureAdmin: %v %v %q", err, created, pw)
	}
	if created, _, _ := users.EnsureAdmin(""); created {
		t.Fatal("second EnsureAdmin must be a no-op")
	}

	res, err := users.Login(" ADMIN ", pw, "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Token == "" || res.User.LastLoginIP != "10.0.0.1" {
		t.Fatalf("unexpected login result: %+v", res)
	}
	u, err := users.Authenticate(res.Token)
	if err != nil || u.Username != "admin" {
		t.Fatalf("Authenticate: %v %v", u, err)
	}

	logs, total, err := audit.List(AuditFilter{Range: "today"})
	if err != nil || total != 1 || logs[0].Action != ActLoginSuccess || logs[0].IP != "10.0.0.1" {
		t.Fatalf("audit after login: %v %d %+v", err, total, logs)
	}
}

func TestLoginLockout(t *testing.T) {
	db := newTestDB(t)
	users, audit := newTestUserService(t, db)
	_, pw, _ := users.EnsureAdmin("")

	for i := 1; i <= 4; i++ {
		_, err := users.Login("admin", "wrong-password-1", "1.1.1.1")
		if status(err) != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %v", i, err)
		}
		if i >= 3 && !strings.Contains(err.Error(), "再错") {
			t.Fatalf("attempt %d should warn about lockout: %v", i, err)
		}
	}
	if _, err := users.Login("admin", "wrong-password-1", "1.1.1.1"); status(err) != http.StatusTooManyRequests {
		t.Fatalf("5th failure must lock: %v", err)
	}
	if _, err := users.Login("admin", pw, "1.1.1.1"); status(err) != http.StatusTooManyRequests {
		t.Fatalf("correct password must be refused while locked: %v", err)
	}
	failed, err := audit.FailedLoginsToday()
	if err != nil || failed["admin"] != 6 {
		t.Fatalf("failed logins today = %v, %v", failed, err)
	}
}

func TestUserManagementGuards(t *testing.T) {
	db := newTestDB(t)
	users, _ := newTestUserService(t, db)
	_, pw, _ := users.EnsureAdmin("")
	login, _ := users.Login("admin", pw, "")
	admin := Actor{UserID: login.User.ID, Username: "admin", Role: model.RoleAdmin}

	if _, err := users.Create(admin, CreateUserInput{Username: "Bad Name", Password: "abcdefgh12", Role: "viewer"}); status(err) != 400 {
		t.Fatalf("invalid username must fail: %v", err)
	}
	if _, err := users.Create(admin, CreateUserInput{Username: "bob", Password: "short", Role: "viewer"}); status(err) != 400 {
		t.Fatalf("weak password must fail: %v", err)
	}
	bob, err := users.Create(admin, CreateUserInput{Username: "Bob", DisplayName: "Bob", Password: "abcdefgh12", Role: "viewer"})
	if err != nil || bob.Username != "bob" {
		t.Fatalf("create bob: %v %+v", err, bob)
	}
	if _, err := users.Create(admin, CreateUserInput{Username: "bob", Password: "abcdefgh12", Role: "viewer"}); status(err) != 409 {
		t.Fatalf("duplicate must conflict: %v", err)
	}

	// Self-protection and last-admin protection.
	disabled := true
	if _, err := users.Update(admin, admin.UserID, UpdateUserInput{Disabled: &disabled}); status(err) != 403 {
		t.Fatalf("must not disable self: %v", err)
	}
	if err := users.Delete(admin, admin.UserID); status(err) != 403 {
		t.Fatalf("must not delete self: %v", err)
	}
	other := Actor{UserID: 999, Username: "ghost", Role: model.RoleAdmin}
	viewer := model.RoleViewer
	if _, err := users.Update(other, admin.UserID, UpdateUserInput{Role: &viewer}); status(err) != 409 {
		t.Fatalf("must keep one admin: %v", err)
	}

	// Disabling bob invalidates his tokens.
	bobLogin, err := users.Login("bob", "abcdefgh12", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.Update(admin, bob.ID, UpdateUserInput{Disabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := users.Authenticate(bobLogin.Token); status(err) != 401 {
		t.Fatalf("disabled user's token must fail: %v", err)
	}
	if _, err := users.Login("bob", "abcdefgh12", ""); status(err) != 403 {
		t.Fatalf("disabled user must not log in: %v", err)
	}

	list, err := users.List(admin)
	if err != nil || len(list) != 2 || !list[0].Me || list[1].Me {
		t.Fatalf("list: %v %+v", err, list)
	}
}

func TestPasswordChangeAndReset(t *testing.T) {
	db := newTestDB(t)
	users, _ := newTestUserService(t, db)
	_, pw, _ := users.EnsureAdmin("")
	login, _ := users.Login("admin", pw, "")
	admin := Actor{UserID: login.User.ID, Username: "admin", Role: model.RoleAdmin}

	if _, err := users.ChangePassword(admin, "wrong", "newPassw0rd1"); status(err) != 400 {
		t.Fatalf("wrong old password must fail: %v", err)
	}
	res, err := users.ChangePassword(admin, pw, "newPassw0rd1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.Authenticate(login.Token); status(err) != 401 {
		t.Fatal("old token must be revoked after password change")
	}
	if _, err := users.Authenticate(res.Token); err != nil {
		t.Fatalf("new token must work: %v", err)
	}

	bob, _ := users.Create(admin, CreateUserInput{Username: "bob", Password: "abcdefgh12", Role: "viewer"})
	newPw, err := users.ResetPassword(admin, bob.ID, "")
	if err != nil || len(newPw) < 10 {
		t.Fatalf("reset: %v %q", err, newPw)
	}
	if _, err := users.Login("bob", newPw, ""); err != nil {
		t.Fatalf("login with reset password: %v", err)
	}
}
