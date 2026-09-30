package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/notify"
)

type sentLog struct {
	mu   sync.Mutex
	msgs []notify.Message
	fail bool
}

func (l *sentLog) send(_ context.Context, _ notify.Target, m notify.Message) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, m)
	if l.fail {
		return errors.New("boom")
	}
	return nil
}

func (l *sentLog) take() []notify.Message {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.msgs
	l.msgs = nil
	return out
}

func TestAlertFireResolveAndNotify(t *testing.T) {
	hour := time.Now().Truncate(time.Hour).Unix()
	fp := &fakeProvider{
		regions: []string{"r1"},
		data: map[string][]cloud.Resource{
			"vm|r1": {vm("a", "r1"), vm("b", "r1"), vm("c", "r1")},
		},
		cpu: map[string]cloud.CPUStat{
			"a": {Hourly: map[int64]float64{hour: 95}},
			"b": {Hourly: map[int64]float64{hour: 97}},
			"c": {Hourly: map[int64]float64{hour: 20}},
		},
	}
	env := newSyncEnv(t, fp)
	db := env.accounts.db
	alerts := NewAlertService(db, env.accounts.box, NewAuditService(db), "https://ops.example.com/")
	log := &sentLog{}
	alerts.send = log.send
	if err := alerts.EnsureRules(); err != nil {
		t.Fatal(err)
	}
	env.syncer.OnFinish = alerts.OnSyncFinished

	ch, err := alerts.CreateChannel(env.admin, ChannelInput{Name: "ops", Type: model.ChannelWebhook, URL: "http://127.0.0.1:9/hook"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.URLMasked != "http://127.0.0.1:9/••••hook" {
		t.Fatalf("masked url: %s", ch.URLMasked)
	}
	if err := alerts.UpdateRule(env.admin, RuleCPUHigh, RuleInput{Enabled: true, Params: map[string]float64{"threshold": 90}, ChannelIDs: []uint{ch.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := alerts.UpdateRule(env.admin, RuleCPUHigh, RuleInput{Enabled: true, Params: map[string]float64{"threshold": 200}}); err == nil {
		t.Fatal("out of range threshold must fail")
	}
	acc, err := env.accounts.Create(context.Background(), env.admin, AccountInput{Name: "fake", Provider: model.ProviderAWS, AccessKeyID: "AKIA1234", AccessKeySecret: "s"})
	if err != nil {
		t.Fatal(err)
	}

	firing := func() map[string]string {
		items, _, err := alerts.Events(EventFilter{Status: model.AlertFiring})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, e := range items {
			out[e.RuleKey+"/"+e.Name] = e.Detail
		}
		return out
	}

	// Sync 1: a and b are hot → one aggregated message with two items.
	env.runSync(t, acc.ID)
	alerts.Wait()
	if f := firing(); len(f) != 2 || f["cpu_high/a"] == "" || f["cpu_high/b"] == "" {
		t.Fatalf("firing after sync 1: %v", f)
	}
	msgs := log.take()
	if len(msgs) != 1 || msgs[0].Event != notify.EventFiring || msgs[0].Total != 2 || len(msgs[0].Items) != 2 ||
		msgs[0].Link != "https://ops.example.com/alerts" || msgs[0].Account != "fake" {
		t.Fatalf("messages after sync 1: %+v", msgs)
	}

	// Sync 2: nothing changes → no duplicate events, no message.
	env.runSync(t, acc.ID)
	alerts.Wait()
	if len(firing()) != 2 || len(log.take()) != 0 {
		t.Fatal("unchanged hits must not fire again")
	}

	// Sync 3: a cools down → resolved message; delivery fails and is recorded.
	fp.mu.Lock()
	fp.cpu["a"] = cloud.CPUStat{Hourly: map[int64]float64{hour: 10}}
	fp.mu.Unlock()
	log.fail = true
	env.runSync(t, acc.ID)
	alerts.Wait()
	log.fail = false
	if f := firing(); len(f) != 1 || f["cpu_high/b"] == "" {
		t.Fatalf("firing after sync 3: %v", f)
	}
	msgs = log.take()
	if len(msgs) != 1 || msgs[0].Event != notify.EventResolved || msgs[0].Items[0].Name != "a" {
		t.Fatalf("messages after sync 3: %+v", msgs)
	}
	resolved, _, _ := alerts.Events(EventFilter{Status: model.AlertResolved})
	if len(resolved) != 1 || resolved[0].ResolvedAt == nil || resolved[0].NotifyError == "" {
		t.Fatalf("resolved event: %+v", resolved)
	}

	// Sync 4: whole sync fails → only sync_failed fires; b stays open (stale data).
	fp.mu.Lock()
	fp.errs = map[string]error{"vm|r1": errors.New("down"), "bucket|": errors.New("down")}
	fp.mu.Unlock()
	job := env.runSync(t, acc.ID)
	alerts.Wait()
	if job.Status != model.JobFailed {
		t.Fatalf("job status %s", job.Status)
	}
	if f := firing(); len(f) != 2 || f["cpu_high/b"] == "" || f["sync_failed/fake"] == "" {
		t.Fatalf("firing after failed sync: %v", f)
	}

	// Disabling a rule resolves its alerts silently.
	log.take()
	if err := alerts.UpdateRule(env.admin, RuleCPUHigh, RuleInput{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	alerts.Wait()
	if f := firing(); len(f) != 1 || f["sync_failed/fake"] == "" {
		t.Fatalf("firing after disabling cpu_high: %v", f)
	}
	if len(log.take()) != 0 {
		t.Fatal("disabling a rule must not notify")
	}

	// Deleting the channel unlinks it from the rules; deleting the account drops its events.
	if err := alerts.DeleteChannel(env.admin, ch.ID); err != nil {
		t.Fatal(err)
	}
	rules, _ := alerts.Rules()
	for _, r := range rules {
		if len(r.ChannelIDs) != 0 {
			t.Fatalf("rule %s still links the deleted channel", r.Key)
		}
	}
	if err := env.accounts.Delete(env.admin, acc.ID); err != nil {
		t.Fatal(err)
	}
	if _, n, _ := alerts.Events(EventFilter{}); n != 0 {
		t.Fatalf("events left after deleting account: %d", n)
	}
}
