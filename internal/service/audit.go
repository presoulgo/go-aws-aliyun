package service

import (
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/presoulgo/go-aws-aliyun/internal/apperr"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// Audit action codes; the web UI maps them to labels.
const (
	ActLoginSuccess      = "login_success"
	ActLoginFailed       = "login_failed"
	ActAccountCreate     = "account_create"
	ActAccountUpdate     = "account_update"
	ActAccountDelete     = "account_delete"
	ActAccountEnable     = "account_enable"
	ActAccountDisable    = "account_disable"
	ActSyncManual        = "sync_manual"
	ActSyncAll           = "sync_all"
	ActSyncCancel        = "sync_cancel"
	ActUserCreate        = "user_create"
	ActUserUpdate        = "user_update"
	ActUserDelete        = "user_delete"
	ActUserEnable        = "user_enable"
	ActUserDisable       = "user_disable"
	ActUserResetPassword = "user_reset_password"
	ActPasswordChange    = "password_change"
)

// AuditService writes and queries audit logs.
type AuditService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewAuditService(db *gorm.DB) *AuditService {
	return &AuditService{db: db, now: time.Now}
}

// AuditEntry is one audit record to write.
type AuditEntry struct {
	Category string
	Action   string
	Failed   bool
	Target   string
	Detail   string
}

// Record writes an audit entry. Failures are logged, never returned: an audit
// problem must not break the operation being audited.
func (s *AuditService) Record(actor Actor, e AuditEntry) {
	result := "success"
	if e.Failed {
		result = "failed"
	}
	log := model.AuditLog{
		UserID:   actor.UserID,
		Username: actor.Username,
		Category: e.Category,
		Action:   e.Action,
		Result:   result,
		Target:   truncate(e.Target, 255),
		Detail:   e.Detail,
		IP:       actor.IP,
	}
	if err := s.db.Create(&log).Error; err != nil {
		slog.Error("写入审计日志失败", "action", e.Action, "err", err)
	}
}

// AuditFilter selects audit logs.
type AuditFilter struct {
	Range    string // today | 7d | 30d | all
	Category string
	Query    string
	Page
}

// List returns matching logs, newest first.
func (s *AuditService) List(f AuditFilter) ([]model.AuditLog, int64, error) {
	q := s.db.Model(&model.AuditLog{})
	now := s.now()
	switch f.Range {
	case "", "7d":
		q = q.Where("created_at >= ?", now.Add(-7*24*time.Hour))
	case "today":
		q = q.Where("created_at >= ?", startOfDay(now))
	case "30d":
		q = q.Where("created_at >= ?", now.Add(-30*24*time.Hour))
	case "all":
	default:
		return nil, 0, apperr.Invalid("无效的时间范围")
	}
	switch f.Category {
	case "", "all":
	case model.AuditLogin, model.AuditAccount, model.AuditSync, model.AuditUser:
		q = q.Where("category = ?", f.Category)
	default:
		return nil, 0, apperr.Invalid("无效的操作类型")
	}
	if kw := strings.TrimSpace(f.Query); kw != "" {
		p := likePattern(kw)
		q = q.Where(`(username LIKE ? ESCAPE '\' OR target LIKE ? ESCAPE '\' OR ip LIKE ? ESCAPE '\' OR detail LIKE ? ESCAPE '\')`, p, p, p, p)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	pg := f.Page.normalize(20, 200)
	var items []model.AuditLog
	err := q.Order("created_at DESC, id DESC").Offset(pg.offset()).Limit(pg.PageSize).Find(&items).Error
	return items, total, err
}

// FailedLoginsToday counts today's failed logins per username.
func (s *AuditService) FailedLoginsToday() (map[string]int, error) {
	type row struct {
		Username string
		N        int
	}
	var rows []row
	err := s.db.Model(&model.AuditLog{}).
		Select("username, COUNT(*) AS n").
		Where("action = ? AND created_at >= ?", ActLoginFailed, startOfDay(s.now())).
		Group("username").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.Username] = r.N
	}
	return out, nil
}

// Cleanup deletes logs older than before.
func (s *AuditService) Cleanup(before time.Time) (int64, error) {
	res := s.db.Where("created_at < ?", before).Delete(&model.AuditLog{})
	return res.RowsAffected, res.Error
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
