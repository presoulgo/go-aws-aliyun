package service

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/presoulgo/go-aws-aliyun/internal/apperr"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// ChangeService queries resource change records written by syncs.
type ChangeService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewChangeService(db *gorm.DB) *ChangeService {
	return &ChangeService{db: db, now: time.Now}
}

// ChangeFilter selects change records.
type ChangeFilter struct {
	Provider   string
	AccountID  uint
	Type       string
	Action     string
	Region     string
	ResourceID string
	Range      string // today | 7d | 30d | all (default)
	Query      string
	Page
}

// ChangeView is a change record with its account name.
type ChangeView struct {
	model.ResourceChange
	AccountName string `json:"account_name"`
}

// List returns matching changes, newest first.
func (s *ChangeService) List(f ChangeFilter) ([]ChangeView, int64, error) {
	q := s.db.Model(&model.ResourceChange{})
	if f.Provider != "" && f.Provider != "all" {
		q = q.Where("resource_changes.provider = ?", f.Provider)
	}
	if f.AccountID > 0 {
		q = q.Where("resource_changes.account_id = ?", f.AccountID)
	}
	if f.Type != "" {
		q = q.Where("resource_changes.type = ?", f.Type)
	}
	if f.Action != "" {
		if f.Action == model.ChangeLeftIdle {
			q = q.Where("(resource_changes.action = ? OR (resource_changes.action = ? AND resource_changes.type IN ?))", model.ChangeLeftIdle, model.ChangeDeleted, model.WasteTypes)
		} else if f.Action == model.ChangeDeleted {
			q = q.Where("resource_changes.action = ? AND resource_changes.type NOT IN ?", model.ChangeDeleted, model.WasteTypes)
		} else {
			q = q.Where("resource_changes.action = ?", f.Action)
		}
	}
	if f.Region != "" {
		q = q.Where("resource_changes.region = ?", f.Region)
	}
	if f.ResourceID != "" {
		q = q.Where("resource_changes.resource_id = ?", f.ResourceID)
	}
	now := s.now()
	switch f.Range {
	case "", "all":
	case "today":
		q = q.Where("resource_changes.created_at >= ?", startOfDay(now).UTC())
	case "7d":
		q = q.Where("resource_changes.created_at >= ?", now.Add(-7*24*time.Hour).UTC())
	case "30d":
		q = q.Where("resource_changes.created_at >= ?", now.Add(-30*24*time.Hour).UTC())
	default:
		return nil, 0, apperr.Invalid("无效的时间范围")
	}
	if kw := strings.TrimSpace(f.Query); kw != "" {
		p := likePattern(kw)
		q = q.Where(`(resource_changes.name LIKE ? ESCAPE '\' OR resource_changes.resource_id LIKE ? ESCAPE '\')`, p, p)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	p := f.Page.normalize(20, 200)
	var rows []ChangeView
	err := q.Select("resource_changes.*, cloud_accounts.name AS account_name").
		Joins("LEFT JOIN cloud_accounts ON cloud_accounts.id = resource_changes.account_id").
		Order("resource_changes.id DESC").Offset(p.offset()).Limit(p.PageSize).Scan(&rows).Error
	// Older syncs stored an idle inventory exit as a deletion. The inventory
	// never included attached disks/bound IPs, so that history cannot prove deletion.
	for i := range rows {
		if rows[i].Action == model.ChangeDeleted && (rows[i].Type == model.TypeDisk || rows[i].Type == model.TypeEIP) {
			rows[i].Action = model.ChangeLeftIdle
		}
	}
	return rows, total, err
}

// Cleanup drops change records older than before.
func (s *ChangeService) Cleanup(before time.Time) (int64, error) {
	res := s.db.Where("created_at < ?", before.UTC()).Delete(&model.ResourceChange{})
	return res.RowsAffected, res.Error
}
