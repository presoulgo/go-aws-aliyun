package store

import (
	"github.com/glebarez/sqlite"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"gorm.io/gorm"
)

func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")
	err = db.AutoMigrate(&model.User{}, &model.CloudAccount{}, &model.Resource{}, &model.SyncJob{}, &model.HostCPUHourly{}, &model.AuditLog{})
	return db, err
}
