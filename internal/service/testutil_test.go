package service

import (
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/presoulgo/go-aws-aliyun/internal/auth"
	"github.com/presoulgo/go-aws-aliyun/internal/store"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close(db) })
	return db
}

func newTestUserService(t *testing.T, db *gorm.DB) (*UserService, *AuditService) {
	t.Helper()
	audit := NewAuditService(db)
	tokens := auth.NewTokens([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	limiter := auth.NewLimiter(5, 5*time.Minute)
	return NewUserService(db, audit, tokens, limiter, 5*time.Minute), audit
}
