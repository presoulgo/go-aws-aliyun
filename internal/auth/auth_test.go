package auth

import (
	"testing"
	"time"
)

func TestPasswordPolicyAndHash(t *testing.T) {
	cases := map[string]bool{
		"short1":          false,
		"onlyletterslong": false,
		"1234567890123":   false,
		"goodPassw0rd":    true,
		"中文密码也可以abc12":    true,
	}
	for pw, ok := range cases {
		if err := ValidatePassword(pw); (err == nil) != ok {
			t.Errorf("ValidatePassword(%q) = %v, want ok=%v", pw, err, ok)
		}
	}
	h, err := HashPassword("goodPassw0rd")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "goodPassw0rd") || CheckPassword(h, "wrong") {
		t.Fatal("bcrypt check mismatch")
	}
	for i := 0; i < 20; i++ {
		pw, err := GeneratePassword(12)
		if err != nil || len(pw) != 12 || ValidatePassword(pw) != nil {
			t.Fatalf("generated password %q invalid: %v", pw, err)
		}
	}
}

func TestTokens(t *testing.T) {
	tk := NewTokens([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	tok, exp, err := tk.Issue(7, "alice", "admin", 3)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(exp) < 59*time.Minute {
		t.Fatalf("expiry too short: %v", exp)
	}
	c, err := tk.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID != 7 || c.Subject != "alice" || c.Role != "admin" || c.Version != 3 {
		t.Fatalf("claims = %+v", c)
	}

	other := NewTokens([]byte("another-secret-another-secret-00"), time.Hour)
	if _, err := other.Parse(tok); err == nil {
		t.Fatal("token signed with another key must fail")
	}

	expired := NewTokens([]byte("0123456789abcdef0123456789abcdef"), time.Minute)
	expired.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	old, _, _ := expired.Issue(1, "bob", "viewer", 0)
	if _, err := tk.Parse(old); err == nil {
		t.Fatal("expired token must fail")
	}
	if _, err := tk.Parse("garbage"); err == nil {
		t.Fatal("garbage must fail")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	l := NewLimiter(3, 5*time.Minute)
	l.now = func() time.Time { return now }

	if rem, _ := l.Fail("Alice"); rem != 2 {
		t.Fatalf("remaining = %d", rem)
	}
	l.Fail("alice")
	rem, until := l.Fail("ALICE")
	if rem != 0 || until.IsZero() {
		t.Fatal("third failure must lock")
	}
	if _, locked := l.LockedUntil("alice"); !locked {
		t.Fatal("alice should be locked")
	}
	if _, locked := l.LockedUntil("bob"); locked {
		t.Fatal("bob should not be locked")
	}
	now = now.Add(6 * time.Minute)
	if _, locked := l.LockedUntil("alice"); locked {
		t.Fatal("lock should expire")
	}
	if rem, _ := l.Fail("alice"); rem != 2 {
		t.Fatalf("counter should restart after lock expiry, remaining = %d", rem)
	}
	l.Reset("alice")
	if rem, _ := l.Fail("alice"); rem != 2 {
		t.Fatalf("reset should clear failures, remaining = %d", rem)
	}
}
