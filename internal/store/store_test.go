package store

import (
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm/clause"

	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

func TestOpenMigratesAndRoundTripsJSONColumns(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "nested", "ops.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer Close(db)

	cpu := 12.5
	now := time.Now().UTC().Truncate(time.Second)
	res := model.Resource{
		AccountID: 1, Provider: model.ProviderAWS, Type: model.TypeVM, Region: "us-east-1",
		ResourceID: "i-123", Name: "web-1", Status: model.StatusRunning,
		Tags:  model.StringMap{"env": "prod"},
		Extra: model.JSONObject{"cpu": 2, "os": "Amazon Linux"},
		CPU1h: &cpu, SyncedAt: now,
	}
	if err := db.Create(&res).Error; err != nil {
		t.Fatal(err)
	}

	// Upsert on the natural key must update rather than duplicate.
	res2 := res
	res2.ID = 0
	res2.Name = "web-1-renamed"
	err = db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}, {Name: "type"}, {Name: "region"}, {Name: "resource_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name"}),
	}).Create(&res2).Error
	if err != nil {
		t.Fatal(err)
	}

	var got []model.Resource
	if err := db.Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].Name != "web-1-renamed" || got[0].Tags["env"] != "prod" || got[0].Extra["os"] != "Amazon Linux" {
		t.Fatalf("unexpected row: %+v", got[0])
	}
	if got[0].CPU1h == nil || *got[0].CPU1h != 12.5 {
		t.Fatalf("cpu_1h not persisted: %v", got[0].CPU1h)
	}

	acc := model.CloudAccount{Name: "a", Provider: model.ProviderAliyun, AccessKeyID: "LTAI", SecretEnc: "x", Regions: model.StringList{"cn-hangzhou"}}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	var loaded model.CloudAccount
	if err := db.First(&loaded, acc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(loaded.Regions) != 1 || loaded.Regions[0] != "cn-hangzhou" || !loaded.Enabled {
		t.Fatalf("unexpected account: %+v", loaded)
	}
}
