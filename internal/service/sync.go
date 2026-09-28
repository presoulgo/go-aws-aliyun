package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/presoulgo/go-aws-aliyun/internal/apperr"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// SyncOptions tunes the sync service.
type SyncOptions struct {
	Concurrency int
	TaskTimeout time.Duration
	KeepJobs    int
}

// SyncService collects resources from the clouds into the local database.
type SyncService struct {
	db       *gorm.DB
	accounts *AccountService
	audit    *AuditService
	opts     SyncOptions
	now      func() time.Time

	// writeMu serializes database writes of concurrent collection tasks so
	// SQLite never sees competing writers.
	writeMu sync.Mutex

	mu      sync.Mutex
	running map[uint]*runningJob // by account id
	wg      sync.WaitGroup
	baseCtx context.Context
}

type runningJob struct {
	jobID  uint
	cancel context.CancelFunc
	done   chan struct{}
}

func NewSyncService(db *gorm.DB, accounts *AccountService, audit *AuditService, opts SyncOptions) *SyncService {
	if opts.Concurrency < 1 {
		opts.Concurrency = 8
	}
	if opts.TaskTimeout <= 0 {
		opts.TaskTimeout = 2 * time.Minute
	}
	if opts.KeepJobs < 1 {
		opts.KeepJobs = 50
	}
	return &SyncService{db: db, accounts: accounts, audit: audit, opts: opts, now: time.Now, running: map[uint]*runningJob{}, baseCtx: context.Background()}
}

// SetBaseContext makes running jobs stop when ctx is cancelled (shutdown).
func (s *SyncService) SetBaseContext(ctx context.Context) { s.baseCtx = ctx }

// MarkInterrupted closes jobs left running by a previous process.
func (s *SyncService) MarkInterrupted() error {
	now := s.now().UTC()
	return s.db.Model(&model.SyncJob{}).Where("status = ?", model.JobRunning).
		Updates(map[string]any{"status": model.JobInterrupted, "finished_at": now, "message": "服务重启，任务中断"}).Error
}

// Trigger starts a sync of one account. If one is already running, that job
// is returned instead.
func (s *SyncService) Trigger(accountID uint, actor Actor, manual bool) (*model.SyncJob, error) {
	acc, err := s.accounts.find(accountID)
	if err != nil {
		return nil, err
	}
	if !acc.Enabled {
		return nil, apperr.Conflict("账号已停用，请先启用再同步")
	}
	job, started, err := s.start(acc, actor, manual)
	if err != nil {
		return nil, err
	}
	if started && manual {
		s.audit.Record(actor, AuditEntry{Category: model.AuditSync, Action: ActSyncManual, Target: acc.Name,
			Detail: fmt.Sprintf("同步任务 #%d · %s", job.ID, regionSummary(acc.Regions, 0))})
	}
	return job, nil
}

// SyncAll starts a sync of every enabled account that is not already syncing.
func (s *SyncService) SyncAll(actor Actor, manual bool) ([]*model.SyncJob, error) {
	var accounts []model.CloudAccount
	if err := s.db.Where("enabled = ?", true).Order("id").Find(&accounts).Error; err != nil {
		return nil, err
	}
	var jobs []*model.SyncJob
	for i := range accounts {
		job, _, err := s.start(&accounts[i], actor, manual)
		if err != nil {
			slog.Error("启动同步失败", "account", accounts[i].Name, "err", err)
			continue
		}
		jobs = append(jobs, job)
	}
	if manual {
		s.audit.Record(actor, AuditEntry{Category: model.AuditSync, Action: ActSyncAll, Target: "全部启用的账号", Detail: fmt.Sprintf("%d 个账号", len(jobs))})
	}
	return jobs, nil
}

func (s *SyncService) start(acc *model.CloudAccount, actor Actor, manual bool) (*model.SyncJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.running[acc.ID]; ok {
		var job model.SyncJob
		if err := s.db.First(&job, r.jobID).Error; err != nil {
			return nil, false, err
		}
		return &job, false, nil
	}
	trigger, by := model.TriggerSchedule, "system"
	if manual {
		trigger, by = model.TriggerManual, actor.Username
	}
	job := model.SyncJob{
		AccountID: acc.ID, Trigger: trigger, TriggeredBy: by, Status: model.JobRunning,
		Stats: model.IntMap{}, Errors: model.TaskErrors{}, StartedAt: s.now().UTC(),
	}
	s.writeMu.Lock()
	err := s.db.Create(&job).Error
	if err == nil {
		err = s.db.Model(&model.CloudAccount{}).Where("id = ?", acc.ID).Update("last_job_id", job.ID).Error
	}
	s.writeMu.Unlock()
	if err != nil {
		return nil, false, err
	}
	ctx, cancel := context.WithCancel(s.baseCtx)
	r := &runningJob{jobID: job.ID, cancel: cancel, done: make(chan struct{})}
	s.running[acc.ID] = r
	s.wg.Add(1)
	accCopy := *acc
	jobCopy := job
	go func() {
		defer s.wg.Done()
		defer close(r.done)
		defer func() {
			s.mu.Lock()
			delete(s.running, accCopy.ID)
			s.mu.Unlock()
			cancel()
		}()
		s.run(ctx, &accCopy, &jobCopy)
	}()
	return &job, true, nil
}

// Cancel stops a running job.
func (s *SyncService) Cancel(jobID uint, actor Actor) error {
	var job model.SyncJob
	if err := s.db.First(&job, jobID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("同步任务不存在")
		}
		return err
	}
	s.mu.Lock()
	r, ok := s.running[job.AccountID]
	s.mu.Unlock()
	if !ok || r.jobID != jobID {
		return apperr.Conflict("该任务已结束")
	}
	r.cancel()
	<-r.done
	var acc model.CloudAccount
	s.db.First(&acc, job.AccountID)
	s.audit.Record(actor, AuditEntry{Category: model.AuditSync, Action: ActSyncCancel, Target: acc.Name, Detail: fmt.Sprintf("同步任务 #%d", jobID)})
	return nil
}

// CancelAccount stops the account's running job, if any, and waits for it.
func (s *SyncService) CancelAccount(accountID uint) {
	s.mu.Lock()
	r, ok := s.running[accountID]
	s.mu.Unlock()
	if !ok {
		return
	}
	r.cancel()
	select {
	case <-r.done:
	case <-time.After(10 * time.Second):
	}
}

// Wait blocks until all running jobs have finished.
func (s *SyncService) Wait() { s.wg.Wait() }

// IsRunning reports whether the account is syncing.
func (s *SyncService) IsRunning(accountID uint) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.running[accountID]
	return ok
}

type task struct {
	typ    string
	region string
	global bool
}

func (s *SyncService) run(ctx context.Context, acc *model.CloudAccount, job *model.SyncJob) {
	start := job.StartedAt
	log := slog.With("account", acc.Name, "job", job.ID)
	log.Info("开始同步")

	prov, err := s.accounts.Provider(acc)
	if err != nil {
		s.finish(acc, job, model.JobFailed, nil, nil, err.Error())
		return
	}
	cred, err := s.accounts.Credential(acc)
	if err != nil {
		s.finish(acc, job, model.JobFailed, nil, nil, err.Error())
		return
	}

	regions := []string(acc.Regions)
	if len(regions) == 0 {
		rctx, cancel := context.WithTimeout(ctx, s.opts.TaskTimeout)
		list, err := prov.ListRegions(rctx, cred)
		cancel()
		if err != nil {
			s.finish(acc, job, statusFor(ctx, model.JobFailed), nil, nil, "获取地域列表失败："+cloud.Describe(err))
			return
		}
		for _, r := range list {
			regions = append(regions, r.ID)
		}
	}

	var tasks []task
	for _, spec := range prov.ResourceTypes() {
		if spec.Global {
			tasks = append(tasks, task{typ: spec.Type, global: true})
			continue
		}
		for _, r := range regions {
			tasks = append(tasks, task{typ: spec.Type, region: r})
		}
	}
	s.writeMu.Lock()
	s.db.Model(job).Update("tasks_total", len(tasks))
	s.writeMu.Unlock()

	var (
		mu     sync.Mutex
		errs   model.TaskErrors
		stats  = model.IntMap{}
		vmIDs  = map[string][]string{} // running VMs per region
		failed atomic.Int32
	)
	g := errgroup.Group{}
	g.SetLimit(s.opts.Concurrency)
	for _, t := range tasks {
		g.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			tctx, cancel := context.WithTimeout(ctx, s.opts.TaskTimeout)
			res, err := prov.Collect(tctx, cred, t.typ, t.region)
			cancel()
			if ctx.Err() != nil {
				return nil
			}
			skipped := err != nil && errors.Is(err, cloud.ErrRegionUnsupported)
			if !skipped && (err == nil || len(res) > 0) {
				if perr := s.persist(acc, t, res, start, err == nil); perr != nil {
					err = errors.Join(err, perr)
				}
			} else if skipped {
				// Nothing lives in a region where the product does not exist.
				_ = s.persist(acc, t, nil, start, true)
			}
			mu.Lock()
			for _, r := range res {
				stats[r.Type]++
				if r.Type == model.TypeVM && r.Status == model.StatusRunning {
					vmIDs[r.Region] = append(vmIDs[r.Region], r.ResourceID)
				}
			}
			if err != nil && !skipped {
				failed.Add(1)
				errs = append(errs, model.TaskError{Region: t.region, Type: t.typ, Message: cloud.Describe(err)})
			}
			mu.Unlock()
			s.writeMu.Lock()
			s.db.Model(&model.SyncJob{}).Where("id = ?", job.ID).Update("tasks_done", gorm.Expr("tasks_done + 1"))
			s.writeMu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	if ctx.Err() != nil {
		s.finish(acc, job, statusFor(ctx, model.JobCancelled), stats, errs, "任务已取消")
		return
	}

	if cpuErrs := s.collectCPU(ctx, acc, prov, cred, vmIDs); len(cpuErrs) > 0 {
		errs = append(errs, cpuErrs...)
	}

	status := model.JobSuccess
	switch {
	case len(tasks) > 0 && int(failed.Load()) == len(tasks):
		status = model.JobFailed
	case len(errs) > 0:
		status = model.JobPartial
	}
	msg := ""
	if len(errs) > 0 {
		msg = errs[0].Message
	}
	s.finish(acc, job, status, stats, errs, msg)
	log.Info("同步结束", "status", status, "errors", len(errs), "stats", map[string]int(stats))
}

func statusFor(ctx context.Context, fallback string) string {
	if ctx.Err() != nil {
		return model.JobCancelled
	}
	return fallback
}

// persist upserts collected resources and, when the task fully succeeded,
// deletes resources of that type and region that disappeared.
func (s *SyncService) persist(acc *model.CloudAccount, t task, res []cloud.Resource, start time.Time, sweep bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	now := s.now().UTC()
	return s.db.Transaction(func(tx *gorm.DB) error {
		if len(res) > 0 {
			rows := make([]model.Resource, 0, len(res))
			for _, r := range res {
				rows = append(rows, toModel(acc, r, now))
			}
			err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "account_id"}, {Name: "type"}, {Name: "region"}, {Name: "resource_id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"provider", "zone", "name", "status", "raw_status", "spec", "private_ip", "public_ip", "vpc_id",
					"charge_type", "expire_at", "cloud_created_at", "tags", "extra", "synced_at", "updated_at",
				}),
			}).CreateInBatches(rows, 100).Error
			if err != nil {
				return err
			}
		}
		if !sweep {
			return nil
		}
		q := tx.Where("account_id = ? AND type = ? AND synced_at < ?", acc.ID, t.typ, start)
		if !t.global {
			q = q.Where("region = ?", t.region)
		}
		return q.Delete(&model.Resource{}).Error
	})
}

func toModel(acc *model.CloudAccount, r cloud.Resource, now time.Time) model.Resource {
	tags := model.StringMap(r.Tags)
	if tags == nil {
		tags = model.StringMap{}
	}
	extra := model.JSONObject(r.Extra)
	if extra == nil {
		extra = model.JSONObject{}
	}
	status := r.Status
	if status == "" {
		status = model.StatusUnknown
	}
	return model.Resource{
		AccountID: acc.ID, Provider: acc.Provider, Type: r.Type, Region: r.Region, Zone: r.Zone,
		ResourceID: r.ResourceID, Name: truncate(r.Name, 255), Status: status, RawStatus: r.RawStatus,
		Spec: r.Spec, PrivateIP: strings.Join(r.PrivateIPs, ","), PublicIP: strings.Join(r.PublicIPs, ","),
		VpcID: r.VpcID, ChargeType: r.ChargeType, ExpireAt: r.ExpireAt, CloudCreatedAt: r.CreatedAt,
		Tags: tags, Extra: extra, SyncedAt: now,
	}
}

// collectCPU stores the CPU snapshot of running hosts and the hourly fleet
// sums used by the dashboard.
func (s *SyncService) collectCPU(ctx context.Context, acc *model.CloudAccount, prov cloud.Provider, cred cloud.Credential, vmIDs map[string][]string) model.TaskErrors {
	var errs model.TaskErrors
	type agg struct {
		sum   float64
		count int
	}
	hourly := map[int64]*agg{}
	now := s.now().UTC()
	for region, ids := range vmIDs {
		if len(ids) == 0 {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, s.opts.TaskTimeout)
		stats, err := prov.CPUSnapshot(cctx, cred, region, ids)
		cancel()
		if err != nil {
			errs = append(errs, model.TaskError{Region: region, Type: "metrics", Message: "CPU 数据：" + cloud.Describe(err)})
			continue
		}
		s.writeMu.Lock()
		err = s.db.Transaction(func(tx *gorm.DB) error {
			for id, st := range stats {
				last, ok1 := st.Last()
				avg, ok2 := st.Average()
				if !ok1 || !ok2 {
					continue
				}
				last, avg = round2(last), round2(avg)
				if err := tx.Model(&model.Resource{}).
					Where("account_id = ? AND type = ? AND region = ? AND resource_id = ?", acc.ID, model.TypeVM, region, id).
					Updates(map[string]any{"cpu_1h": last, "cpu_24h": avg, "metrics_at": now}).Error; err != nil {
					return err
				}
				for h, v := range st.Hourly {
					a := hourly[h]
					if a == nil {
						a = &agg{}
						hourly[h] = a
					}
					a.sum += v
					a.count++
				}
			}
			return nil
		})
		s.writeMu.Unlock()
		if err != nil {
			errs = append(errs, model.TaskError{Region: region, Type: "metrics", Message: err.Error()})
		}
	}
	if len(hourly) == 0 {
		return errs
	}
	rows := make([]model.HostCPUHourly, 0, len(hourly))
	for h, a := range hourly {
		rows = append(rows, model.HostCPUHourly{AccountID: acc.ID, Provider: acc.Provider, Hour: h, Sum: a.sum, Count: a.count})
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}, {Name: "hour"}},
		DoUpdates: clause.AssignmentColumns([]string{"provider", "sum", "count"}),
	}).CreateInBatches(rows, 100).Error
	if err != nil {
		errs = append(errs, model.TaskError{Type: "metrics", Message: err.Error()})
	}
	return errs
}

func (s *SyncService) finish(acc *model.CloudAccount, job *model.SyncJob, status string, stats model.IntMap, errs model.TaskErrors, msg string) {
	if stats == nil {
		stats = model.IntMap{}
	}
	if errs == nil {
		errs = model.TaskErrors{}
	}
	now := s.now().UTC()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := s.db.Model(&model.SyncJob{}).Where("id = ?", job.ID).Updates(map[string]any{
		"status": status, "stats": stats, "errors": errs, "error_count": len(errs),
		"message": msg, "finished_at": now,
	}).Error
	if err != nil {
		slog.Error("更新同步任务失败", "job", job.ID, "err", err)
	}
	accErr := ""
	if status != model.JobSuccess {
		accErr = msg
	}
	err = s.db.Model(&model.CloudAccount{}).Where("id = ?", acc.ID).Updates(map[string]any{
		"last_sync_at": now, "last_sync_status": status, "last_sync_error": accErr,
	}).Error
	if err != nil {
		slog.Error("更新账号同步状态失败", "account", acc.ID, "err", err)
	}
	// Keep the newest jobs per account.
	var keep []uint
	s.db.Model(&model.SyncJob{}).Where("account_id = ?", acc.ID).Order("id DESC").Limit(s.opts.KeepJobs).Pluck("id", &keep)
	if len(keep) == s.opts.KeepJobs {
		s.db.Where("account_id = ? AND id NOT IN ?", acc.ID, keep).Delete(&model.SyncJob{})
	}
}

// Jobs lists jobs of an account (all accounts when accountID is 0).
func (s *SyncService) Jobs(accountID uint, p Page) ([]model.SyncJob, int64, error) {
	q := s.db.Model(&model.SyncJob{})
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	p = p.normalize(10, 100)
	var jobs []model.SyncJob
	err := q.Order("id DESC").Offset(p.offset()).Limit(p.PageSize).Find(&jobs).Error
	return jobs, total, err
}

// Job returns one job.
func (s *SyncService) Job(id uint) (*model.SyncJob, error) {
	var job model.SyncJob
	if err := s.db.First(&job, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("同步任务不存在")
		}
		return nil, err
	}
	return &job, nil
}

// SyncStatus summarizes sync health for the top bar.
type SyncStatus struct {
	State           string     `json:"state"` // ok | partial | failed | none
	Running         int        `json:"running"`
	Accounts        int        `json:"accounts"`
	ProblemAccounts int        `json:"problem_accounts"`
	LastFinishedAt  *time.Time `json:"last_finished_at"`
}

// Status computes the current sync health.
func (s *SyncService) Status() (*SyncStatus, error) {
	var accounts []model.CloudAccount
	if err := s.db.Where("enabled = ?", true).Find(&accounts).Error; err != nil {
		return nil, err
	}
	st := &SyncStatus{State: "none", Accounts: len(accounts)}
	s.mu.Lock()
	st.Running = len(s.running)
	s.mu.Unlock()
	failed := 0
	for _, a := range accounts {
		if a.LastSyncAt != nil && (st.LastFinishedAt == nil || a.LastSyncAt.After(*st.LastFinishedAt)) {
			t := *a.LastSyncAt
			st.LastFinishedAt = &t
		}
		switch a.LastSyncStatus {
		case model.JobPartial, model.JobInterrupted, model.JobCancelled:
			st.ProblemAccounts++
		case model.JobFailed:
			st.ProblemAccounts++
			failed++
		}
	}
	switch {
	case len(accounts) == 0 || st.LastFinishedAt == nil:
		st.State = "none"
	case failed > 0 && failed == len(accounts):
		st.State = "failed"
	case st.ProblemAccounts > 0:
		st.State = "partial"
	default:
		st.State = "ok"
	}
	return st, nil
}

// CleanupCPUHistory drops hourly CPU rows older than before.
func (s *SyncService) CleanupCPUHistory(before time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.db.Where("hour < ?", before.Unix()).Delete(&model.HostCPUHourly{}).Error
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
