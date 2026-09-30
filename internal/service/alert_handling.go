package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/presoulgo/go-aws-aliyun/internal/apperr"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/notify"
)

type EventHandlingInput struct {
	Acknowledged *bool   `json:"acknowledged"`
	Note         *string `json:"note"`
	// nil keeps the current silence; 0 clears it.
	SilenceMinutes *int `json:"silence_minutes"`
}

func (s *AlertService) findEvent(id uint) (*model.AlertEvent, error) {
	var event model.AlertEvent
	if err := s.db.First(&event, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("告警不存在")
		}
		return nil, err
	}
	return &event, nil
}

func (s *AlertService) HandleEvent(actor Actor, id uint, in EventHandlingInput) error {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	// Serialize with notification delivery: a completed silence takes effect
	// before any following delivery, including manual retries.
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	event, err := s.findEvent(id)
	if err != nil {
		return err
	}
	updates := map[string]any{}
	now := s.now().UTC()
	if in.Acknowledged != nil {
		if !*in.Acknowledged {
			updates["acknowledged_at"], updates["acknowledged_by"] = nil, ""
		} else if event.AcknowledgedAt == nil {
			updates["acknowledged_at"], updates["acknowledged_by"] = now, actor.Username
		}
	}
	if in.Note != nil {
		if len([]rune(*in.Note)) > 2000 {
			return apperr.Invalid("备注不能超过 2000 字")
		}
		updates["note"] = strings.TrimSpace(*in.Note)
	}
	if in.SilenceMinutes != nil {
		if *in.SilenceMinutes < 0 || *in.SilenceMinutes > 7*24*60 {
			return apperr.Invalid("静默时间须在 0 至 7 天之间")
		}
		updates["silenced_until"] = nil
		if *in.SilenceMinutes > 0 {
			updates["silenced_until"] = now.Add(time.Duration(*in.SilenceMinutes) * time.Minute)
		}
	}
	if len(updates) == 0 && in.Acknowledged == nil {
		return apperr.Invalid("请填写处理内容")
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if len(updates) > 0 {
			if err := tx.Model(&model.AlertEvent{}).Where("id = ?", id).Updates(updates).Error; err != nil {
				return err
			}
		}
		if in.SilenceMinutes != nil {
			// Keep the target's silence consistent across recurring events.
			return tx.Model(&model.AlertEvent{}).Where("account_id = ? AND rule_key = ? AND target_key = ?", event.AccountID, event.RuleKey, event.TargetKey).Update("silenced_until", updates["silenced_until"]).Error
		}
		return nil
	}); err != nil {
		return err
	}
	s.audit.Record(actor, AuditEntry{Category: model.AuditAlert, Action: "alert_event_handle", Target: fmt.Sprintf("告警 #%d", id), Detail: "更新确认、备注或静默设置"})
	return nil
}

func (s *AlertService) RetryEvent(ctx context.Context, actor Actor, id uint) (int, error) {
	if _, err := s.findEvent(id); err != nil {
		return 0, err
	}
	var rows []model.AlertDelivery
	// Pending deliveries can remain after a service restart. Each send is
	// serialized and rereads status, so simultaneous retries do not duplicate it.
	err := s.db.Where("status IN ? AND EXISTS (SELECT 1 FROM json_each(alert_deliveries.event_ids) WHERE value = ?)", []string{"failed", "pending"}, id).Order("id").Find(&rows).Error
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, apperr.Conflict("没有可重试的通知；历史通知未保存投递内容时无法重发")
	}
	n := 0
	var failures []string
	for _, row := range rows {
		if err := s.deliver(ctx, row.ID); err != nil {
			failures = append(failures, err.Error())
		} else {
			n++
		}
	}
	s.audit.Record(actor, AuditEntry{Category: model.AuditAlert, Action: "alert_notify_retry", Target: fmt.Sprintf("告警 #%d", id), Detail: fmt.Sprintf("%d 条成功，%d 条失败", n, len(failures))})
	if len(failures) > 0 {
		return n, apperr.Invalid("重发未全部成功：" + strings.Join(failures, "；"))
	}
	return n, nil
}

func (s *AlertService) deliver(ctx context.Context, id uint) error {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	var row model.AlertDelivery
	if err := s.db.First(&row, id).Error; err != nil {
		return err
	}
	if row.Status == "sent" {
		return nil
	}
	var events []model.AlertEvent
	if err := s.db.Where("id IN ?", []uint(row.EventIDs)).Find(&events).Error; err != nil {
		return err
	}
	if len(events) == 0 {
		return apperr.NotFound("告警已删除")
	}
	for _, event := range events {
		if event.SilencedUntil != nil && event.SilencedUntil.After(s.now()) {
			return apperr.Conflict("该聚合通知包含静默中的告警，请先取消静默再重发")
		}
	}
	c, err := s.findChannel(row.ChannelID)
	if err == nil && !c.Enabled {
		err = apperr.Conflict("通知渠道已停用")
	}
	var msg notify.Message
	var target notify.Target
	if err == nil {
		err = json.Unmarshal([]byte(row.Payload), &msg)
	}
	if err == nil {
		target, err = s.target(c)
	}
	if err == nil {
		err = s.send(ctx, target, msg)
	}
	status, detail := "sent", ""
	if err != nil {
		status = "failed"
		detail = err.Error()
		// Network errors can include the full credential-bearing webhook URL.
		if target.URL != "" {
			detail = strings.ReplaceAll(detail, target.URL, "[通知地址]")
		}
		if target.Secret != "" {
			detail = strings.ReplaceAll(detail, target.Secret, "[签名密钥]")
		}
		if c != nil {
			detail = c.Name + "：" + detail
		}
		err = errors.New(detail)
	}
	if dbErr := s.db.Model(&row).Updates(map[string]any{"status": status, "error": detail, "attempts": gorm.Expr("attempts + 1")}).Error; dbErr != nil {
		return dbErr
	}
	for _, event := range events {
		var failed []model.AlertDelivery
		if dbErr := s.db.Where("status = ? AND EXISTS (SELECT 1 FROM json_each(alert_deliveries.event_ids) WHERE value = ?)", "failed", event.ID).Order("id").Find(&failed).Error; dbErr != nil {
			return dbErr
		}
		parts := []string{}
		for _, f := range failed {
			parts = append(parts, f.Error)
		}
		if dbErr := s.db.Model(&model.AlertEvent{}).Where("id = ?", event.ID).Update("notify_error", strings.Join(parts, "；")).Error; dbErr != nil {
			return dbErr
		}
	}
	return err
}
