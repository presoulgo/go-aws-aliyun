package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/notify"
)

func TestAlertFreshnessAndPartialSync(t *testing.T) {
	now := time.Now().UTC()
	hour := now.Truncate(time.Hour).Unix()
	fp := &fakeProvider{regions: []string{"r1", "r2"}, data: map[string][]cloud.Resource{
		"vm|r1": {vm("a", "r1")}, "vm|r2": {vm("b", "r2")},
	}, cpu: map[string]cloud.CPUStat{
		"a": {Hourly: map[int64]float64{hour: 95}}, "b": {Hourly: map[int64]float64{hour: 95}},
	}}
	env := newSyncEnv(t, fp)
	db := env.accounts.db
	alerts := NewAlertService(db, env.accounts.box, NewAuditService(db), "")
	if err := alerts.EnsureRules(); err != nil {
		t.Fatal(err)
	}
	if err := alerts.UpdateRule(env.admin, RuleIdleHost, RuleInput{Enabled: true, Params: map[string]float64{"threshold": 5}}); err != nil {
		t.Fatal(err)
	}
	env.syncer.OnFinish = alerts.OnSyncFinished
	acc, err := env.accounts.Create(context.Background(), env.admin, AccountInput{Name: "partial", Provider: model.ProviderAWS, AccessKeyID: "test", AccessKeySecret: "test"})
	if err != nil {
		t.Fatal(err)
	}
	env.runSync(t, acc.ID)
	before, _ := env.syncer.Health(acc.ID)
	var lastSuccess time.Time
	for _, scope := range before {
		if scope.Type == "vm" && scope.Region == "r1" {
			lastSuccess = *scope.LastSuccessAt
		}
	}

	// One page arrived before a regional failure. Those data must not create
	// alerts; healthy regions continue to resolve their alerts.
	fp.data["vm|r1"] = []cloud.Resource{vm("c", "r1")}
	fp.errs = map[string]error{"vm|r1": errors.New("second page unavailable")}
	fp.cpu["c"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 1}}
	fp.cpu["b"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 20}}
	if job := env.runSync(t, acc.ID); job.Status != model.JobPartial {
		t.Fatalf("status %s", job.Status)
	}
	open, _, err := alerts.Events(EventFilter{Status: model.AlertFiring})
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("expected a + sync failure, got %+v", open)
	}
	for _, e := range open {
		if e.RuleKey == RuleSyncFailed && (!strings.Contains(e.Detail, "r1 / vm") || !strings.Contains(e.Detail, "上次成功")) {
			t.Fatalf("missing failure scope/timestamp: %s", e.Detail)
		}
		if e.RuleKey == RuleIdleHost {
			t.Fatal("partial data fired idle alert")
		}
	}
	health, _ := env.syncer.Health(acc.ID)
	for _, scope := range health {
		if scope.Type == "vm" && scope.Region == "r1" && (scope.Status != model.JobFailed || scope.LastSuccessAt == nil || !scope.LastSuccessAt.Equal(lastSuccess)) {
			t.Fatalf("lost last successful time: %+v", scope)
		}
	}
	if _, total, err := env.res.List(ResourceFilter{Idle: true}); err != nil || total != 0 {
		t.Fatalf("unavailable data classified idle: %d %v", total, err)
	}

	// Cloud returns old samples after collection recovers. They retain their
	// real timestamp and must not fire an idle alert or resolve a hot alert.
	fp.errs = nil
	fp.data["vm|r1"] = []cloud.Resource{vm("a", "r1")}
	fp.cpu["a"] = cloud.CPUStat{Hourly: map[int64]float64{hour - 3*3600: 1}}
	env.runSync(t, acc.ID)
	open, _, _ = alerts.Events(EventFilter{Status: model.AlertFiring})
	if len(open) != 1 || open[0].ResourceID != "a" || open[0].RuleKey != RuleCPUHigh {
		t.Fatalf("stale data resolved/fired: %+v", open)
	}
	var r model.Resource
	if err := db.Where("account_id = ? AND resource_id = ?", acc.ID, "a").First(&r).Error; err != nil {
		t.Fatal(err)
	}
	if r.MetricsAt == nil || r.MetricsAt.Unix() != hour-3*3600 {
		t.Fatalf("old sample stamped fresh: %+v", r.MetricsAt)
	}
	if _, total, err := env.res.List(ResourceFilter{Idle: true}); err != nil || total != 0 {
		t.Fatalf("stale data classified idle: %d %v", total, err)
	}

	// Actual fresh low CPU recovers the high alert and creates the idle alert.
	fp.cpu["a"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 1}}
	env.runSync(t, acc.ID)
	open, _, _ = alerts.Events(EventFilter{Status: model.AlertFiring})
	if len(open) != 1 || open[0].RuleKey != RuleIdleHost {
		t.Fatalf("fresh data not evaluated: %+v", open)
	}
	alerts.now = func() time.Time { return now.Add(3 * time.Hour) }
	alerts.EvaluateAccount(acc.ID)
	open, _, _ = alerts.Events(EventFilter{Status: model.AlertFiring})
	if len(open) != 1 || open[0].RuleKey != RuleIdleHost {
		t.Fatal("age alone recovered an existing alert")
	}
	// A newly observed stopped state can recover CPU alerts without new CPU
	// samples: stopped machines are not queried for CPU by the synchronizer.
	alerts.now = time.Now
	fp.data["vm|r1"][0].Status = model.StatusStopped
	env.runSync(t, acc.ID)
	if _, n, _ := alerts.Events(EventFilter{Status: model.AlertFiring}); n != 0 {
		t.Fatal("stopped machine retained CPU alert")
	}
}

func TestAlertHandlingAndDurableRetry(t *testing.T) {
	var good, bad atomic.Int32
	var failing atomic.Bool
	failing.Store(true)
	var original, retried notify.Message
	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { good.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer goodServer.Close()
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg notify.Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Error(err)
		}
		bad.Add(1)
		if failing.Load() {
			original = msg
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			retried = msg
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer badServer.Close()
	hour := time.Now().Truncate(time.Hour).Unix()
	fp := &fakeProvider{regions: []string{"r1"}, data: map[string][]cloud.Resource{"vm|r1": {vm("a", "r1"), vm("b", "r1")}}, cpu: map[string]cloud.CPUStat{
		"a": {Hourly: map[int64]float64{hour: 95}}, "b": {Hourly: map[int64]float64{hour: 97}},
	}}
	env := newSyncEnv(t, fp)
	db := env.accounts.db
	alerts := NewAlertService(db, env.accounts.box, NewAuditService(db), "")
	alerts.send = notify.Send
	if err := alerts.EnsureRules(); err != nil {
		t.Fatal(err)
	}
	ch1, err := alerts.CreateChannel(env.admin, ChannelInput{Name: "good", Type: model.ChannelWebhook, URL: goodServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	ch2, err := alerts.CreateChannel(env.admin, ChannelInput{Name: "bad", Type: model.ChannelWebhook, URL: badServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := alerts.UpdateRule(env.admin, RuleCPUHigh, RuleInput{Enabled: true, Params: map[string]float64{"threshold": 90}, ChannelIDs: []uint{ch1.ID, ch2.ID}}); err != nil {
		t.Fatal(err)
	}
	env.syncer.OnFinish = alerts.OnSyncFinished
	acc, err := env.accounts.Create(context.Background(), env.admin, AccountInput{Name: "retry", Provider: model.ProviderAWS, AccessKeyID: "test", AccessKeySecret: "test"})
	if err != nil {
		t.Fatal(err)
	}
	env.runSync(t, acc.ID)
	alerts.Wait()
	events, _, _ := alerts.Events(EventFilter{Status: model.AlertFiring})
	if len(events) != 2 || !events[0].NotifyRetryable || events[0].NotifyError == "" {
		t.Fatalf("failed delivery not saved: %+v", events)
	}

	// Recreate the service to prove retry uses the persisted payload and sends
	// only the failed destination once for the original aggregate.
	restarted := NewAlertService(db, env.accounts.box, NewAuditService(db), "")
	restarted.send = notify.Send
	failing.Store(false)
	block := 60
	if err := restarted.HandleEvent(env.admin, events[0].ID, EventHandlingInput{SilenceMinutes: &block}); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.RetryEvent(context.Background(), env.admin, events[0].ID); err == nil || good.Load() != 1 || bad.Load() != 1 {
		t.Fatal("retry bypassed silence")
	}
	unmute := 0
	if err := restarted.HandleEvent(env.admin, events[0].ID, EventHandlingInput{SilenceMinutes: &unmute}); err != nil {
		t.Fatal(err)
	}
	if n, err := restarted.RetryEvent(context.Background(), env.admin, events[0].ID); err != nil || n != 1 {
		t.Fatalf("retry: %d %v", n, err)
	}
	if good.Load() != 1 || bad.Load() != 2 || retried.Total != 2 || retried.Event != original.Event || !retried.At.Equal(original.At) {
		t.Fatalf("retry changed original message or repeated successful channel: %+v", retried)
	}
	events, _, _ = restarted.Events(EventFilter{Status: model.AlertFiring})
	for _, e := range events {
		if e.NotifyError != "" || e.NotifyRetryable {
			t.Fatalf("failure not cleared: %+v", e)
		}
	}
	if _, err := restarted.RetryEvent(context.Background(), env.admin, events[0].ID); err == nil {
		t.Fatal("successful delivery retry allowed")
	}

	ack, note, silence := true, "处理中", 60
	for _, e := range events {
		if err := restarted.HandleEvent(env.admin, e.ID, EventHandlingInput{Acknowledged: &ack, Note: &note, SilenceMinutes: &silence}); err != nil {
			t.Fatal(err)
		}
	}
	// Recovered and recurring targets remain silent during the time window.
	env.syncer.OnFinish = restarted.OnSyncFinished
	fp.cpu["a"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 10}}
	fp.cpu["b"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 10}}
	env.runSync(t, acc.ID)
	restarted.Wait()
	resolved, _, _ := restarted.Events(EventFilter{Status: model.AlertResolved})
	if len(resolved) != 2 || resolved[0].AcknowledgedBy != "admin" || resolved[0].Note != note || resolved[0].SilencedUntil == nil {
		t.Fatalf("handling lost: %+v", resolved)
	}
	fp.cpu["a"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 95}}
	fp.cpu["b"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 97}}
	env.runSync(t, acc.ID)
	restarted.Wait()
	events, _, _ = restarted.Events(EventFilter{Status: model.AlertFiring})
	if len(events) != 2 || events[0].SilencedUntil == nil || good.Load() != 1 || bad.Load() != 2 {
		t.Fatalf("silence did not cover recurrence: %+v", events)
	}
	// Explicitly clearing silence allows the following recovery notification.
	zero := 0
	for _, e := range events {
		if err := restarted.HandleEvent(env.admin, e.ID, EventHandlingInput{SilenceMinutes: &zero}); err != nil {
			t.Fatal(err)
		}
	}
	fp.cpu["a"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 10}}
	fp.cpu["b"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 10}}
	env.runSync(t, acc.ID)
	restarted.Wait()
	if good.Load() != 2 || bad.Load() != 3 {
		t.Fatal("unmuting did not restore subsequent notifications")
	}
	// Silence expires against the server clock without a timer or a restart.
	fp.cpu["a"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 95}}
	fp.cpu["b"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 97}}
	env.runSync(t, acc.ID)
	restarted.Wait()
	events, _, _ = restarted.Events(EventFilter{Status: model.AlertFiring})
	ten := 10
	for _, e := range events {
		if err := restarted.HandleEvent(env.admin, e.ID, EventHandlingInput{SilenceMinutes: &ten}); err != nil {
			t.Fatal(err)
		}
	}
	future := time.Now().Add(11 * time.Minute)
	restarted.now = func() time.Time { return future }
	env.syncer.now = restarted.now
	fp.cpu["a"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 10}}
	fp.cpu["b"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 10}}
	env.runSync(t, acc.ID)
	restarted.Wait()
	if good.Load() != 4 || bad.Load() != 5 {
		t.Fatal("expired silence suppressed future recovery")
	}
	if _, err := restarted.Cleanup(future.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var remaining int64
	if err := db.Model(&model.AlertDelivery{}).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("orphaned delivery records: %d %v", remaining, err)
	}
}

func TestWasteExitIsNotDeletion(t *testing.T) {
	env := newSyncEnv(t, &fakeProvider{})
	acc, err := env.accounts.Create(context.Background(), env.admin, AccountInput{Name: "waste", Provider: model.ProviderAWS, AccessKeyID: "test", AccessKeySecret: "test"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	stored := model.CloudAccount{ID: acc.ID, Provider: model.ProviderAWS, LastSyncAt: &now}
	env.syncer.now = func() time.Time { return now }
	for _, typ := range []string{model.TypeDisk, model.TypeEIP} {
		tsk := task{typ: typ, region: "r1"}
		resource := cloud.Resource{Type: typ, Region: "r1", ResourceID: typ, Name: typ}
		if err := env.syncer.persist(&stored, 1, tsk, []cloud.Resource{resource}, now, true); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
		if err := env.syncer.persist(&stored, 2, tsk, nil, now, true); err != nil {
			t.Fatal(err)
		}
	}
	changes, _, err := NewChangeService(env.accounts.db).List(ChangeFilter{Action: model.ChangeLeftIdle})
	if err != nil || len(changes) != 2 {
		t.Fatalf("expected two idle exits: %+v %v", changes, err)
	}
	if _, n, _ := NewChangeService(env.accounts.db).List(ChangeFilter{Action: model.ChangeDeleted}); n != 0 {
		t.Fatal("idle exit recorded as deletion")
	}
	// Old history is also displayed with the corrected inventory semantics.
	if err := env.accounts.db.Model(&model.ResourceChange{}).Where("action = ?", model.ChangeLeftIdle).Update("action", model.ChangeDeleted).Error; err != nil {
		t.Fatal(err)
	}
	legacy, n, err := NewChangeService(env.accounts.db).List(ChangeFilter{Action: model.ChangeLeftIdle})
	if err != nil || n != 2 || legacy[0].Action != model.ChangeLeftIdle {
		t.Fatalf("legacy exit still labeled deleted: %+v %v", legacy, err)
	}
}
