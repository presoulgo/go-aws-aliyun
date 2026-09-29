package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

func TestDiffResource(t *testing.T) {
	t1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	// Same instant read back from the database with sub-second noise and another zone.
	t1b := t1.Add(300 * time.Microsecond).In(time.FixedZone("CST", 8*3600))
	t2 := t1.AddDate(1, 0, 0)
	base := model.Resource{
		Name: "web-01", Status: model.StatusRunning, Spec: "t3.small", PrivateIP: "10.0.0.1", PublicIP: "",
		ChargeType: model.ChargePrepaid, ExpireAt: &t1, Tags: model.StringMap{"env": "prod", "team": "a"},
		Extra: model.JSONObject{"size_bytes": 1},
	}
	with := func(f func(r *model.Resource)) model.Resource {
		r := base
		r.Tags = model.StringMap{}
		for k, v := range base.Tags {
			r.Tags[k] = v
		}
		f(&r)
		return r
	}
	cases := []struct {
		name string
		cur  model.Resource
		want model.FieldChanges
	}{
		{"unchanged", with(func(r *model.Resource) {}), nil},
		{"extra ignored", with(func(r *model.Resource) { r.Extra = model.JSONObject{"size_bytes": 2} }), nil},
		{"expire same instant", with(func(r *model.Resource) { r.ExpireAt = &t1b }), nil},
		{"status and spec", with(func(r *model.Resource) { r.Status = model.StatusStopped; r.Spec = "t3.large" }),
			model.FieldChanges{{Field: "status", Old: "running", New: "stopped"}, {Field: "spec", Old: "t3.small", New: "t3.large"}}},
		{"renewed", with(func(r *model.Resource) { r.ExpireAt = &t2 }),
			model.FieldChanges{{Field: "expire_at", Old: "2026-10-01T00:00:00Z", New: "2027-10-01T00:00:00Z"}}},
		{"converted to postpaid", with(func(r *model.Resource) { r.ChargeType = model.ChargePostpaid; r.ExpireAt = nil }),
			model.FieldChanges{{Field: "charge_type", Old: "prepaid", New: "postpaid"}, {Field: "expire_at", Old: "2026-10-01T00:00:00Z", New: ""}}},
		{"ip bound", with(func(r *model.Resource) { r.PublicIP = "1.2.3.4" }),
			model.FieldChanges{{Field: "public_ip", Old: "", New: "1.2.3.4"}}},
		{"tags added, changed, removed", with(func(r *model.Resource) {
			r.Tags["env"] = "staging"
			r.Tags["owner"] = "ops"
			delete(r.Tags, "team")
		}), model.FieldChanges{
			{Field: "tags.env", Old: "prod", New: "staging"},
			{Field: "tags.owner", Old: "", New: "ops"},
			{Field: "tags.team", Old: "a", New: ""},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := diffResource(base, c.cur)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestSyncRecordsChanges(t *testing.T) {
	fp := &fakeProvider{
		regions: []string{"r1", "r2"},
		data: map[string][]cloud.Resource{
			"vm|r1":   {vm("a", "r1"), vm("b", "r1")},
			"vm|r2":   {vm("c", "r2")},
			"bucket|": {{Type: model.TypeBucket, Region: "r1", ResourceID: "x", Name: "x", Status: model.StatusRunning, Extra: map[string]any{"size_bytes": 1}}},
		},
	}
	env := newSyncEnv(t, fp)
	changes := NewChangeService(env.accounts.db)
	acc, err := env.accounts.Create(context.Background(), env.admin, AccountInput{Name: "fake", Provider: model.ProviderAWS, AccessKeyID: "AKIA1234", AccessKeySecret: "s"})
	if err != nil {
		t.Fatal(err)
	}

	// The first sync of an account does not report everything as created.
	env.runSync(t, acc.ID)
	if _, total, _ := changes.List(ChangeFilter{}); total != 0 {
		t.Fatalf("first sync recorded %d changes", total)
	}

	a := vm("a", "r1")
	a.Status = model.StatusStopped
	a.Tags = map[string]string{"env": "staging"}
	fp.mu.Lock()
	fp.data["vm|r1"] = []cloud.Resource{a, vm("d", "r1")}
	fp.data["vm|r2"] = nil
	fp.errs = map[string]error{"vm|r2": errors.New("boom")}
	fp.data["bucket|"][0].Extra = map[string]any{"size_bytes": 2}
	fp.mu.Unlock()
	job := env.runSync(t, acc.ID)

	items, total, err := changes.List(ChangeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ChangeView{}
	for _, c := range items {
		got[c.ResourceID] = c
		if c.JobID != job.ID || c.AccountName != "fake" {
			t.Fatalf("bad change row: %+v", c)
		}
	}
	// c lives in the failed region and must not be reported as deleted; the
	// bucket only changed in Extra.
	if total != 3 || got["a"].Action != model.ChangeUpdated || got["b"].Action != model.ChangeDeleted || got["d"].Action != model.ChangeCreated {
		t.Fatalf("unexpected changes: %+v", items)
	}
	want := model.FieldChanges{{Field: "status", Old: "running", New: "stopped"}, {Field: "tags.env", Old: "prod", New: "staging"}}
	if !reflect.DeepEqual(got["a"].Changes, want) {
		t.Fatalf("a changes: %+v", got["a"].Changes)
	}

	if _, n, _ := changes.List(ChangeFilter{ResourceID: "a", AccountID: acc.ID, Type: model.TypeVM}); n != 1 {
		t.Fatalf("filter by resource: %d", n)
	}
	if err := env.accounts.Delete(env.admin, acc.ID); err != nil {
		t.Fatal(err)
	}
	if _, n, _ := changes.List(ChangeFilter{}); n != 0 {
		t.Fatalf("changes left after deleting account: %d", n)
	}
}
