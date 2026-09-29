package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/presoulgo/go-aws-aliyun/internal/apperr"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/notify"
	"github.com/presoulgo/go-aws-aliyun/internal/secret"
)

// Built-in alert rule keys.
const (
	RuleCPUHigh    = "cpu_high"
	RuleIdleHost   = "idle_host"
	RuleExpiring   = "expiring"
	RuleWaste      = "waste"
	RuleSyncFailed = "sync_failed"
)

// Alert audit actions.
const (
	ActAlertRuleUpdate = "alert_rule_update"
	ActChannelCreate   = "channel_create"
	ActChannelUpdate   = "channel_update"
	ActChannelDelete   = "channel_delete"
)

type paramDef struct {
	Key      string
	Default  float64
	Min, Max float64
}

type ruleDef struct {
	Key         string
	Name        string
	Description string
	Enabled     bool
	Params      []paramDef
}

var ruleDefs = []ruleDef{
	{RuleCPUHigh, "主机 CPU 过高", "运行中主机近 1 小时 CPU 使用率不低于阈值", true,
		[]paramDef{{"threshold", 90, 1, 100}}},
	{RuleIdleHost, "闲置主机", "运行中主机 24 小时 CPU 均值低于阈值", false,
		[]paramDef{{"threshold", 5, 0.1, 100}}},
	{RuleExpiring, "资源即将到期", "包年包月资源在设定天数内到期", true,
		[]paramDef{{"days", 7, 1, 365}}},
	{RuleWaste, "未挂载云盘 / 未绑定弹性 IP", "存在仍在计费的闲置云盘或弹性 IP", true, nil},
	{RuleSyncFailed, "同步失败", "账号同步失败（全部采集任务都失败）", true, nil},
}

func findRuleDef(key string) *ruleDef {
	for i := range ruleDefs {
		if ruleDefs[i].Key == key {
			return &ruleDefs[i]
		}
	}
	return nil
}

// AlertService evaluates built-in rules against the local database and
// sends notifications.
type AlertService struct {
	db          *gorm.DB
	box         *secret.Box
	audit       *AuditService
	externalURL string
	now         func() time.Time
	send        func(context.Context, notify.Target, notify.Message) error

	evalMu  sync.Mutex
	pending sync.WaitGroup
}

func NewAlertService(db *gorm.DB, box *secret.Box, audit *AuditService, externalURL string) *AlertService {
	return &AlertService{
		db: db, box: box, audit: audit, externalURL: strings.TrimRight(externalURL, "/"),
		now: time.Now, send: notify.SendWithRetry,
	}
}

// EnsureRules creates missing built-in rules with their defaults.
func (s *AlertService) EnsureRules() error {
	for _, d := range ruleDefs {
		params := model.JSONObject{}
		for _, p := range d.Params {
			params[p.Key] = p.Default
		}
		r := model.AlertRule{Key: d.Key, Enabled: d.Enabled, Params: params, ChannelIDs: model.UintList{}}
		if err := s.db.Where(model.AlertRule{Key: d.Key}).FirstOrCreate(&r).Error; err != nil {
			return err
		}
	}
	return nil
}

// Wait blocks until queued notifications have been sent.
func (s *AlertService) Wait() { s.pending.Wait() }

// ---------- rules ----------

// RuleView is a rule with its description and current firing count.
type RuleView struct {
	Key         string             `json:"key"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Enabled     bool               `json:"enabled"`
	Params      map[string]float64 `json:"params"`
	ChannelIDs  []uint             `json:"channel_ids"`
	Firing      int64              `json:"firing"`
}

// RuleInput updates a rule.
type RuleInput struct {
	Enabled    bool               `json:"enabled"`
	Params     map[string]float64 `json:"params"`
	ChannelIDs []uint             `json:"channel_ids"`
}

func (s *AlertService) loadRules() (map[string]model.AlertRule, error) {
	var rows []model.AlertRule
	if err := s.db.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]model.AlertRule, len(rows))
	for _, r := range rows {
		out[r.Key] = r
	}
	return out, nil
}

func ruleParams(d *ruleDef, r model.AlertRule) map[string]float64 {
	out := map[string]float64{}
	for _, p := range d.Params {
		out[p.Key] = p.Default
		if v, ok := r.Params[p.Key].(float64); ok {
			out[p.Key] = v
		}
	}
	return out
}

// Rules lists the built-in rules.
func (s *AlertService) Rules() ([]RuleView, error) {
	rules, err := s.loadRules()
	if err != nil {
		return nil, err
	}
	type count struct {
		RuleKey string
		N       int64
	}
	var counts []count
	if err := s.db.Model(&model.AlertEvent{}).Select("rule_key, COUNT(*) AS n").
		Where("status = ?", model.AlertFiring).Group("rule_key").Scan(&counts).Error; err != nil {
		return nil, err
	}
	firing := map[string]int64{}
	for _, c := range counts {
		firing[c.RuleKey] = c.N
	}
	out := make([]RuleView, 0, len(ruleDefs))
	for i := range ruleDefs {
		d := &ruleDefs[i]
		r := rules[d.Key]
		ids := []uint(r.ChannelIDs)
		if ids == nil {
			ids = []uint{}
		}
		out = append(out, RuleView{
			Key: d.Key, Name: d.Name, Description: d.Description, Enabled: r.Enabled,
			Params: ruleParams(d, r), ChannelIDs: ids, Firing: firing[d.Key],
		})
	}
	return out, nil
}

// UpdateRule changes a rule. Disabling it silently resolves its open alerts;
// saving an enabled rule re-evaluates every account right away.
func (s *AlertService) UpdateRule(actor Actor, key string, in RuleInput) error {
	d := findRuleDef(key)
	if d == nil {
		return apperr.NotFound("告警规则不存在")
	}
	params := model.JSONObject{}
	var parts []string
	for _, p := range d.Params {
		v, ok := in.Params[p.Key]
		if !ok {
			v = p.Default
		}
		if v < p.Min || v > p.Max {
			return apperr.Invalid(fmt.Sprintf("阈值需在 %g~%g 之间", p.Min, p.Max))
		}
		params[p.Key] = v
		parts = append(parts, fmt.Sprintf("%s=%g", p.Key, v))
	}
	ids := model.UintList{}
	if len(in.ChannelIDs) > 0 {
		var n int64
		if err := s.db.Model(&model.NotifyChannel{}).Where("id IN ?", in.ChannelIDs).Count(&n).Error; err != nil {
			return err
		}
		if int(n) != len(in.ChannelIDs) {
			return apperr.Invalid("通知渠道不存在，请刷新后重试")
		}
		ids = in.ChannelIDs
	}
	rule := model.AlertRule{Key: key, Enabled: in.Enabled, Params: params, ChannelIDs: ids}
	if err := s.db.Save(&rule).Error; err != nil {
		return err
	}
	state := "开启"
	if !in.Enabled {
		state = "关闭"
		if err := s.db.Model(&model.AlertEvent{}).Where("rule_key = ? AND status = ?", key, model.AlertFiring).
			Updates(map[string]any{"status": model.AlertResolved, "resolved_at": s.now().UTC()}).Error; err != nil {
			return err
		}
	}
	parts = append([]string{state}, append(parts, fmt.Sprintf("%d 个渠道", len(ids)))...)
	s.audit.Record(actor, AuditEntry{Category: model.AuditAlert, Action: ActAlertRuleUpdate, Target: d.Name, Detail: strings.Join(parts, " · ")})
	if in.Enabled {
		s.EvaluateAll()
	}
	return nil
}

// ---------- channels ----------

// ChannelView is a channel with its URL masked.
type ChannelView struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	URLMasked string    `json:"url_masked"`
	HasSecret bool      `json:"has_secret"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ChannelInput creates or updates a channel. Empty URL or secret on update
// keeps the stored value; ClearSecret removes the Feishu secret.
type ChannelInput struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	Secret      string `json:"secret"`
	ClearSecret bool   `json:"clear_secret"`
	Enabled     *bool  `json:"enabled"`
}

func maskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return secret.Mask(raw)
	}
	r := []rune(raw)
	tail := ""
	if len(r) > 4 {
		tail = string(r[len(r)-4:])
	}
	return u.Scheme + "://" + u.Host + "/••••" + tail
}

func (s *AlertService) channelView(c *model.NotifyChannel) ChannelView {
	v := ChannelView{ID: c.ID, Name: c.Name, Type: c.Type, HasSecret: c.SecretEnc != "", Enabled: c.Enabled,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if u, err := s.box.Decrypt(c.URLEnc); err == nil {
		v.URLMasked = maskURL(u)
	}
	return v
}

// Channels lists notification channels.
func (s *AlertService) Channels() ([]ChannelView, error) {
	var rows []model.NotifyChannel
	if err := s.db.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ChannelView, 0, len(rows))
	for i := range rows {
		out = append(out, s.channelView(&rows[i]))
	}
	return out, nil
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func channelTypeLabel(t string) string {
	if t == model.ChannelFeishu {
		return "飞书"
	}
	return "Webhook"
}

// CreateChannel adds a channel.
func (s *AlertService) CreateChannel(actor Actor, in ChannelInput) (*ChannelView, error) {
	in.Name, in.URL, in.Secret = strings.TrimSpace(in.Name), strings.TrimSpace(in.URL), strings.TrimSpace(in.Secret)
	if in.Name == "" {
		return nil, apperr.Invalid("请填写渠道名称")
	}
	if in.Type != model.ChannelFeishu && in.Type != model.ChannelWebhook {
		return nil, apperr.Invalid("不支持的渠道类型")
	}
	if !validURL(in.URL) {
		return nil, apperr.Invalid("请填写以 http:// 或 https:// 开头的地址")
	}
	c := model.NotifyChannel{Name: truncate(in.Name, 64), Type: in.Type, Enabled: in.Enabled == nil || *in.Enabled}
	var err error
	if c.URLEnc, err = s.box.Encrypt(in.URL); err != nil {
		return nil, err
	}
	if in.Secret != "" && in.Type == model.ChannelFeishu {
		if c.SecretEnc, err = s.box.Encrypt(in.Secret); err != nil {
			return nil, err
		}
	}
	if err := s.db.Create(&c).Error; err != nil {
		return nil, err
	}
	s.audit.Record(actor, AuditEntry{Category: model.AuditAlert, Action: ActChannelCreate, Target: c.Name, Detail: channelTypeLabel(c.Type)})
	v := s.channelView(&c)
	return &v, nil
}

func (s *AlertService) findChannel(id uint) (*model.NotifyChannel, error) {
	var c model.NotifyChannel
	if err := s.db.First(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("通知渠道不存在")
		}
		return nil, err
	}
	return &c, nil
}

// UpdateChannel edits a channel.
func (s *AlertService) UpdateChannel(actor Actor, id uint, in ChannelInput) (*ChannelView, error) {
	c, err := s.findChannel(id)
	if err != nil {
		return nil, err
	}
	in.Name, in.URL, in.Secret = strings.TrimSpace(in.Name), strings.TrimSpace(in.URL), strings.TrimSpace(in.Secret)
	if in.Name != "" {
		c.Name = truncate(in.Name, 64)
	}
	if in.URL != "" {
		if !validURL(in.URL) {
			return nil, apperr.Invalid("请填写以 http:// 或 https:// 开头的地址")
		}
		if c.URLEnc, err = s.box.Encrypt(in.URL); err != nil {
			return nil, err
		}
	}
	switch {
	case in.ClearSecret || c.Type != model.ChannelFeishu:
		c.SecretEnc = ""
	case in.Secret != "":
		if c.SecretEnc, err = s.box.Encrypt(in.Secret); err != nil {
			return nil, err
		}
	}
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	}
	if err := s.db.Save(c).Error; err != nil {
		return nil, err
	}
	s.audit.Record(actor, AuditEntry{Category: model.AuditAlert, Action: ActChannelUpdate, Target: c.Name, Detail: channelTypeLabel(c.Type)})
	v := s.channelView(c)
	return &v, nil
}

// DeleteChannel removes a channel and unlinks it from the rules.
func (s *AlertService) DeleteChannel(actor Actor, id uint) error {
	c, err := s.findChannel(id)
	if err != nil {
		return err
	}
	rules, err := s.loadRules()
	if err != nil {
		return err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(c).Error; err != nil {
			return err
		}
		for _, r := range rules {
			kept := model.UintList{}
			for _, cid := range r.ChannelIDs {
				if cid != id {
					kept = append(kept, cid)
				}
			}
			if len(kept) != len(r.ChannelIDs) {
				if err := tx.Model(&model.AlertRule{}).Where("\"key\" = ?", r.Key).Update("channel_ids", kept).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.audit.Record(actor, AuditEntry{Category: model.AuditAlert, Action: ActChannelDelete, Target: c.Name, Detail: channelTypeLabel(c.Type)})
	return nil
}

func (s *AlertService) target(c *model.NotifyChannel) (notify.Target, error) {
	u, err := s.box.Decrypt(c.URLEnc)
	if err != nil {
		return notify.Target{}, err
	}
	t := notify.Target{Type: c.Type, URL: u}
	if c.SecretEnc != "" {
		if t.Secret, err = s.box.Decrypt(c.SecretEnc); err != nil {
			return notify.Target{}, err
		}
	}
	return t, nil
}

// TestChannel sends a test message and reports the delivery error.
func (s *AlertService) TestChannel(ctx context.Context, id uint) error {
	c, err := s.findChannel(id)
	if err != nil {
		return err
	}
	t, err := s.target(c)
	if err != nil {
		return err
	}
	msg := notify.Message{
		Event: notify.EventTest, Rule: "test", RuleName: "测试消息", Title: "云枢告警通知测试",
		Items: []notify.Item{{Name: "渠道「" + c.Name + "」配置正确，后续告警会发送到这里"}}, Total: 1,
		At: s.now().UTC(), Link: s.link(),
	}
	if err := notify.Send(ctx, t, msg); err != nil {
		return apperr.Invalid("发送失败：" + err.Error())
	}
	return nil
}

func (s *AlertService) link() string {
	if s.externalURL == "" {
		return ""
	}
	return s.externalURL + "/alerts"
}

// ---------- events ----------

// EventFilter selects alert events.
type EventFilter struct {
	Status    string
	Rule      string
	AccountID uint
	Page
}

// EventView is an event with its account and rule name.
type EventView struct {
	model.AlertEvent
	AccountName string `json:"account_name"`
	Provider    string `json:"provider"`
	RuleName    string `json:"rule_name"`
}

// Events lists alert events, open ones first.
func (s *AlertService) Events(f EventFilter) ([]EventView, int64, error) {
	q := s.db.Model(&model.AlertEvent{})
	switch f.Status {
	case "", "all":
	case model.AlertFiring, model.AlertResolved:
		q = q.Where("alert_events.status = ?", f.Status)
	default:
		return nil, 0, apperr.Invalid("无效的告警状态")
	}
	if f.Rule != "" {
		q = q.Where("alert_events.rule_key = ?", f.Rule)
	}
	if f.AccountID > 0 {
		q = q.Where("alert_events.account_id = ?", f.AccountID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	p := f.Page.normalize(20, 200)
	var rows []EventView
	err := q.Select("alert_events.*, cloud_accounts.name AS account_name, cloud_accounts.provider AS provider").
		Joins("LEFT JOIN cloud_accounts ON cloud_accounts.id = alert_events.account_id").
		Order("alert_events.status = 'firing' DESC, alert_events.fired_at DESC, alert_events.id DESC").
		Offset(p.offset()).Limit(p.PageSize).Scan(&rows).Error
	for i := range rows {
		if d := findRuleDef(rows[i].RuleKey); d != nil {
			rows[i].RuleName = d.Name
		}
	}
	return rows, total, err
}

// Cleanup drops resolved events older than before.
func (s *AlertService) Cleanup(before time.Time) (int64, error) {
	res := s.db.Where("status = ? AND resolved_at < ?", model.AlertResolved, before.UTC()).Delete(&model.AlertEvent{})
	return res.RowsAffected, res.Error
}

// ---------- evaluation ----------

// OnSyncFinished is called after a sync job of the account ended.
func (s *AlertService) OnSyncFinished(accountID uint, status string) {
	if status == model.JobCancelled {
		return
	}
	s.EvaluateAccount(accountID)
}

// EvaluateAll re-evaluates every account.
func (s *AlertService) EvaluateAll() {
	var ids []uint
	if err := s.db.Model(&model.CloudAccount{}).Order("id").Pluck("id", &ids).Error; err != nil {
		slog.Error("告警评估失败", "err", err)
		return
	}
	for _, id := range ids {
		s.EvaluateAccount(id)
	}
}

// EvaluateAccount recomputes the alerts of one account: new hits fire,
// hits that went away resolve.
func (s *AlertService) EvaluateAccount(accountID uint) {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	if err := s.evaluate(accountID); err != nil {
		slog.Error("告警评估失败", "account", accountID, "err", err)
	}
}

type alertHit struct {
	key, typ, region, resourceID, name, value string
}

func (s *AlertService) evaluate(accountID uint) error {
	var acc model.CloudAccount
	if err := s.db.First(&acc, accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	rules, err := s.loadRules()
	if err != nil {
		return err
	}
	// After a failed sync the resource data is stale: only report the failure.
	failed := acc.LastSyncStatus == model.JobFailed
	now := s.now().UTC()
	for i := range ruleDefs {
		d := &ruleDefs[i]
		r, ok := rules[d.Key]
		if !ok || !r.Enabled || (failed && d.Key != RuleSyncFailed) {
			continue
		}
		hits, err := s.hits(&acc, d.Key, ruleParams(d, r), now)
		if err != nil {
			return err
		}
		var open []model.AlertEvent
		if err := s.db.Where("rule_key = ? AND account_id = ? AND status = ?", d.Key, acc.ID, model.AlertFiring).
			Find(&open).Error; err != nil {
			return err
		}
		openBy := make(map[string]model.AlertEvent, len(open))
		for _, e := range open {
			openBy[e.TargetKey] = e
		}
		var fired []model.AlertEvent
		for _, h := range hits {
			if _, ok := openBy[h.key]; ok {
				delete(openBy, h.key)
				continue
			}
			fired = append(fired, model.AlertEvent{
				RuleKey: d.Key, AccountID: acc.ID, Status: model.AlertFiring, TargetKey: h.key,
				ResourceType: h.typ, Region: h.region, ResourceID: h.resourceID, Name: truncate(h.name, 255),
				Detail: h.value, FiredAt: now,
			})
		}
		var resolved []model.AlertEvent
		for _, e := range open {
			if _, left := openBy[e.TargetKey]; left {
				resolved = append(resolved, e)
			}
		}
		if len(fired) == 0 && len(resolved) == 0 {
			continue
		}
		err = s.db.Transaction(func(tx *gorm.DB) error {
			if len(fired) > 0 {
				if err := tx.CreateInBatches(&fired, 100).Error; err != nil {
					return err
				}
			}
			if len(resolved) > 0 {
				ids := make([]uint, len(resolved))
				for i, e := range resolved {
					ids[i] = e.ID
				}
				return tx.Model(&model.AlertEvent{}).Where("id IN ?", ids).
					Updates(map[string]any{"status": model.AlertResolved, "resolved_at": now}).Error
			}
			return nil
		})
		if err != nil {
			return err
		}
		s.notify(d, &acc, r.ChannelIDs, notify.EventFiring, fired, now)
		s.notify(d, &acc, r.ChannelIDs, notify.EventResolved, resolved, now)
	}
	return nil
}

func (s *AlertService) hits(acc *model.CloudAccount, key string, p map[string]float64, now time.Time) ([]alertHit, error) {
	if key == RuleSyncFailed {
		if acc.LastSyncStatus != model.JobFailed {
			return nil, nil
		}
		return []alertHit{{key: "account", name: acc.Name, value: truncate(acc.LastSyncError, 300)}}, nil
	}
	q := s.db.Where("account_id = ?", acc.ID)
	switch key {
	case RuleCPUHigh:
		q = q.Where("type = ? AND status = ? AND cpu_1h >= ?", model.TypeVM, model.StatusRunning, p["threshold"])
	case RuleIdleHost:
		q = q.Where("type = ? AND status = ? AND cpu_24h IS NOT NULL AND cpu_24h < ?", model.TypeVM, model.StatusRunning, p["threshold"])
	case RuleExpiring:
		q = q.Where("expire_at IS NOT NULL AND expire_at >= ? AND expire_at <= ?", now, now.Add(time.Duration(p["days"])*24*time.Hour))
	case RuleWaste:
		q = q.Where("type IN ?", model.WasteTypes)
	default:
		return nil, nil
	}
	var rows []model.Resource
	if err := q.Order("name, resource_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]alertHit, 0, len(rows))
	for _, r := range rows {
		h := alertHit{
			key: r.Type + ":" + r.Region + ":" + r.ResourceID, typ: r.Type, region: r.Region,
			resourceID: r.ResourceID, name: r.Name,
		}
		if h.name == "" {
			h.name = r.ResourceID
		}
		switch key {
		case RuleCPUHigh:
			h.value = fmt.Sprintf("近 1 小时 CPU %.1f%%", deref(r.CPU1h))
		case RuleIdleHost:
			h.value = fmt.Sprintf("24 小时 CPU 均值 %.1f%%", deref(r.CPU24h))
		case RuleExpiring:
			days := int(r.ExpireAt.Sub(now).Hours() / 24)
			h.value = fmt.Sprintf("%d 天后到期（%s）", days, r.ExpireAt.Local().Format("2006-01-02"))
		case RuleWaste:
			label := "未挂载云盘"
			if r.Type == model.TypeEIP {
				label = "未绑定弹性 IP"
			}
			h.value = strings.TrimSuffix(label+" · "+r.Spec, " · ")
		}
		out = append(out, h)
	}
	return out, nil
}

func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// notify sends one aggregated message per channel in the background and
// records delivery failures on the events.
func (s *AlertService) notify(d *ruleDef, acc *model.CloudAccount, channelIDs []uint, event string, events []model.AlertEvent, at time.Time) {
	if len(events) == 0 || len(channelIDs) == 0 {
		return
	}
	var channels []model.NotifyChannel
	if err := s.db.Where("id IN ? AND enabled = ?", channelIDs, true).Find(&channels).Error; err != nil || len(channels) == 0 {
		return
	}
	prefix := "【告警】"
	if event == notify.EventResolved {
		prefix = "【恢复】"
	}
	msg := notify.Message{
		Event: event, Rule: d.Key, RuleName: d.Name, Title: fmt.Sprintf("%s%s · %d 项", prefix, d.Name, len(events)),
		Account: acc.Name, Provider: acc.Provider, Total: len(events), At: at, Link: s.link(),
	}
	if d.Key == RuleSyncFailed {
		msg.Title = prefix + d.Name + " · " + acc.Name
	}
	for i, e := range events {
		if i == notify.MaxItems {
			break
		}
		msg.Items = append(msg.Items, notify.Item{Name: e.Name, ResourceID: e.ResourceID, Region: e.Region, Value: e.Detail})
	}
	ids := make([]uint, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	s.pending.Add(1)
	go func() {
		defer s.pending.Done()
		var errs []string
		for i := range channels {
			c := &channels[i]
			t, err := s.target(c)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				err = s.send(ctx, t, msg)
				cancel()
			}
			if err != nil {
				slog.Warn("告警通知发送失败", "channel", c.Name, "rule", d.Key, "err", err)
				errs = append(errs, c.Name+"："+err.Error())
			}
		}
		if len(errs) > 0 {
			s.db.Model(&model.AlertEvent{}).Where("id IN ?", ids).Update("notify_error", strings.Join(errs, "；"))
		}
	}()
}
