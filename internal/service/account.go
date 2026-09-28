package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/presoulgo/go-aws-aliyun/internal/apperr"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/secret"
)

var regionRe = regexp.MustCompile(`^[a-z0-9-]{3,40}$`)

// AccountService manages cloud accounts and their credentials.
type AccountService struct {
	db       *gorm.DB
	box      *secret.Box
	registry *cloud.Registry
	audit    *AuditService
	// timeout bounds credential checks against the cloud.
	timeout time.Duration
}

func NewAccountService(db *gorm.DB, box *secret.Box, registry *cloud.Registry, audit *AuditService) *AccountService {
	return &AccountService{db: db, box: box, registry: registry, audit: audit, timeout: 30 * time.Second}
}

// AccountInput is the payload for creating or updating an account.
type AccountInput struct {
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	Partition       string   `json:"partition"`
	AccessKeyID     string   `json:"access_key_id"`
	AccessKeySecret string   `json:"access_key_secret"`
	RoleARN         string   `json:"role_arn"`
	Regions         []string `json:"regions"`
	Remark          string   `json:"remark"`
	Enabled         *bool    `json:"enabled"`
}

// JobSummary is the latest sync job shown next to an account.
type JobSummary struct {
	ID         uint       `json:"id"`
	Status     string     `json:"status"`
	Trigger    string     `json:"trigger"`
	TasksTotal int        `json:"tasks_total"`
	TasksDone  int        `json:"tasks_done"`
	ErrorCount int        `json:"error_count"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// AccountView is an account as returned by the API; secrets never leave.
type AccountView struct {
	ID              uint        `json:"id"`
	Name            string      `json:"name"`
	Provider        string      `json:"provider"`
	Partition       string      `json:"partition"`
	AccessKeyMasked string      `json:"access_key_masked"`
	RoleARN         string      `json:"role_arn"`
	Regions         []string    `json:"regions"`
	Enabled         bool        `json:"enabled"`
	Remark          string      `json:"remark"`
	CloudAccountUID string      `json:"cloud_account_uid"`
	LastSyncAt      *time.Time  `json:"last_sync_at"`
	LastSyncStatus  string      `json:"last_sync_status"`
	LastSyncError   string      `json:"last_sync_error"`
	ResourceCount   int64       `json:"resource_count"`
	RegionCount     int64       `json:"region_count"`
	LastJob         *JobSummary `json:"last_job"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

// TestResult is returned by a connection test.
type TestResult struct {
	AccountUID string         `json:"account_uid"`
	Arn        string         `json:"arn"`
	Regions    []cloud.Region `json:"regions"`
}

func (s *AccountService) view(a model.CloudAccount, counts map[uint]int64, regions map[uint]int64, jobs map[uint]*model.SyncJob) AccountView {
	v := AccountView{
		ID: a.ID, Name: a.Name, Provider: a.Provider, Partition: a.Partition,
		AccessKeyMasked: secret.Mask(a.AccessKeyID), RoleARN: a.RoleARN, Regions: []string(a.Regions),
		Enabled: a.Enabled, Remark: a.Remark, CloudAccountUID: a.CloudAccountUID,
		LastSyncAt: a.LastSyncAt, LastSyncStatus: a.LastSyncStatus, LastSyncError: a.LastSyncError,
		ResourceCount: counts[a.ID], RegionCount: regions[a.ID], CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	if v.Regions == nil {
		v.Regions = []string{}
	}
	if j, ok := jobs[a.ID]; ok {
		v.LastJob = &JobSummary{
			ID: j.ID, Status: j.Status, Trigger: j.Trigger, TasksTotal: j.TasksTotal, TasksDone: j.TasksDone,
			ErrorCount: j.ErrorCount, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt,
		}
	}
	return v
}

// List returns all accounts with resource counts and their latest job.
func (s *AccountService) List() ([]AccountView, error) {
	var accounts []model.CloudAccount
	if err := s.db.Order("provider ASC, id ASC").Find(&accounts).Error; err != nil {
		return nil, err
	}
	counts, regions, err := s.resourceCounts()
	if err != nil {
		return nil, err
	}
	jobs, err := s.latestJobs()
	if err != nil {
		return nil, err
	}
	out := make([]AccountView, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, s.view(a, counts, regions, jobs))
	}
	return out, nil
}

// Get returns one account.
func (s *AccountService) Get(id uint) (*AccountView, error) {
	a, err := s.find(id)
	if err != nil {
		return nil, err
	}
	counts, regions, err := s.resourceCounts()
	if err != nil {
		return nil, err
	}
	jobs, err := s.latestJobs()
	if err != nil {
		return nil, err
	}
	v := s.view(*a, counts, regions, jobs)
	return &v, nil
}

func (s *AccountService) resourceCounts() (map[uint]int64, map[uint]int64, error) {
	type row struct {
		AccountID uint
		N         int64
		R         int64
	}
	var rows []row
	if err := s.db.Model(&model.Resource{}).Select("account_id, COUNT(*) AS n, COUNT(DISTINCT region) AS r").Group("account_id").Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	counts, regions := map[uint]int64{}, map[uint]int64{}
	for _, r := range rows {
		counts[r.AccountID], regions[r.AccountID] = r.N, r.R
	}
	return counts, regions, nil
}

func (s *AccountService) latestJobs() (map[uint]*model.SyncJob, error) {
	var jobs []model.SyncJob
	sub := s.db.Model(&model.SyncJob{}).Select("MAX(id)").Group("account_id")
	if err := s.db.Where("id IN (?)", sub).Find(&jobs).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]*model.SyncJob, len(jobs))
	for i := range jobs {
		out[jobs[i].AccountID] = &jobs[i]
	}
	return out, nil
}

func (s *AccountService) find(id uint) (*model.CloudAccount, error) {
	var a model.CloudAccount
	if err := s.db.First(&a, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("云账号不存在")
		}
		return nil, err
	}
	return &a, nil
}

// Credential decrypts the account's credential for provider calls.
func (s *AccountService) Credential(a *model.CloudAccount) (cloud.Credential, error) {
	plain, err := s.box.Decrypt(a.SecretEnc)
	if err != nil {
		return cloud.Credential{}, err
	}
	return cloud.Credential{
		AccessKeyID:     a.AccessKeyID,
		AccessKeySecret: plain,
		RoleARN:         a.RoleARN,
		Partition:       a.Partition,
		CacheKey:        fmt.Sprintf("account:%d:%d", a.ID, a.UpdatedAt.UnixNano()),
	}, nil
}

// Provider returns the provider for an account.
func (s *AccountService) Provider(a *model.CloudAccount) (cloud.Provider, error) {
	p, ok := s.registry.Get(a.Provider)
	if !ok {
		return nil, apperr.Invalid("不支持的云厂商：" + a.Provider)
	}
	return p, nil
}

func (s *AccountService) normalize(in *AccountInput, creating bool) error {
	in.Name = strings.TrimSpace(in.Name)
	in.AccessKeyID = strings.TrimSpace(in.AccessKeyID)
	in.RoleARN = strings.TrimSpace(in.RoleARN)
	in.Remark = strings.TrimSpace(in.Remark)
	if in.Name == "" || len([]rune(in.Name)) > 64 {
		return apperr.Invalid("账号名称不能为空，且不超过 64 个字符")
	}
	if len([]rune(in.Remark)) > 200 {
		return apperr.Invalid("备注不超过 200 个字符")
	}
	if creating {
		if _, ok := s.registry.Get(in.Provider); !ok {
			return apperr.Invalid("请选择云厂商")
		}
		if in.AccessKeyID == "" || in.AccessKeySecret == "" {
			return apperr.Invalid("请填写 AccessKey ID 和 AccessKey Secret")
		}
	}
	switch in.Provider {
	case model.ProviderAWS:
		if in.Partition == "" {
			in.Partition = model.PartitionAWS
		}
		if in.Partition != model.PartitionAWS && in.Partition != model.PartitionAWSCN {
			return apperr.Invalid("AWS 分区无效")
		}
		if in.RoleARN != "" && !strings.HasPrefix(in.RoleARN, "arn:aws") {
			return apperr.Invalid("AWS 角色 ARN 应以 arn:aws 开头")
		}
	case model.ProviderAliyun:
		in.Partition = ""
		if in.RoleARN != "" && !strings.HasPrefix(in.RoleARN, "acs:ram::") {
			return apperr.Invalid("阿里云 RAM 角色 ARN 应以 acs:ram:: 开头")
		}
	}
	regions := make([]string, 0, len(in.Regions))
	for _, r := range in.Regions {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !regionRe.MatchString(r) {
			return apperr.Invalid("地域格式无效：" + r)
		}
		if !slices.Contains(regions, r) {
			regions = append(regions, r)
		}
	}
	in.Regions = regions
	return nil
}

// Test checks credentials that have not been saved yet.
func (s *AccountService) Test(ctx context.Context, in AccountInput) (*TestResult, error) {
	if in.Name == "" {
		in.Name = "test"
	}
	if err := s.normalize(&in, true); err != nil {
		return nil, err
	}
	p, _ := s.registry.Get(in.Provider)
	return s.check(ctx, p, cloud.Credential{
		AccessKeyID: in.AccessKeyID, AccessKeySecret: in.AccessKeySecret,
		RoleARN: in.RoleARN, Partition: in.Partition,
	})
}

// TestExisting checks a saved account, optionally with changed fields from
// the edit form (an empty secret keeps the stored one).
func (s *AccountService) TestExisting(ctx context.Context, id uint, in *AccountInput) (*TestResult, error) {
	a, err := s.find(id)
	if err != nil {
		return nil, err
	}
	cred, err := s.Credential(a)
	if err != nil {
		return nil, err
	}
	if in != nil {
		if v := strings.TrimSpace(in.AccessKeyID); v != "" {
			cred.AccessKeyID = v
		}
		if in.AccessKeySecret != "" {
			cred.AccessKeySecret = in.AccessKeySecret
		}
		if in.Partition != "" && a.Provider == model.ProviderAWS {
			cred.Partition = in.Partition
		}
		cred.RoleARN = strings.TrimSpace(in.RoleARN)
		cred.CacheKey = ""
	}
	p, err := s.Provider(a)
	if err != nil {
		return nil, err
	}
	return s.check(ctx, p, cred)
}

func (s *AccountService) check(ctx context.Context, p cloud.Provider, cred cloud.Credential) (*TestResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	info, err := p.Validate(ctx, cred)
	if err != nil {
		return nil, apperr.Upstream("测试连接失败：" + cloud.Describe(err)).Wrap(err)
	}
	regions, err := p.ListRegions(ctx, cred)
	if err != nil {
		return nil, apperr.Upstream("凭证有效，但获取地域列表失败：" + cloud.Describe(err)).Wrap(err)
	}
	return &TestResult{AccountUID: info.AccountUID, Arn: info.Arn, Regions: regions}, nil
}

// Create validates the credential against the cloud and stores the account.
func (s *AccountService) Create(ctx context.Context, actor Actor, in AccountInput) (*AccountView, error) {
	if err := s.normalize(&in, true); err != nil {
		return nil, err
	}
	p, _ := s.registry.Get(in.Provider)
	res, err := s.check(ctx, p, cloud.Credential{AccessKeyID: in.AccessKeyID, AccessKeySecret: in.AccessKeySecret, RoleARN: in.RoleARN, Partition: in.Partition})
	if err != nil {
		return nil, err
	}
	enc, err := s.box.Encrypt(in.AccessKeySecret)
	if err != nil {
		return nil, err
	}
	a := model.CloudAccount{
		Name: in.Name, Provider: in.Provider, Partition: in.Partition,
		AccessKeyID: in.AccessKeyID, SecretEnc: enc, RoleARN: in.RoleARN,
		Regions: model.StringList(in.Regions), Enabled: true, Remark: in.Remark,
		CloudAccountUID: res.AccountUID,
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
	if err := s.db.Create(&a).Error; err != nil {
		return nil, err
	}
	detail := fmt.Sprintf("测试连接通过 · %s", regionSummary(in.Regions, len(res.Regions)))
	if in.RoleARN != "" {
		detail += " · " + roleKind(in.Provider)
	}
	s.audit.Record(actor, AuditEntry{Category: model.AuditAccount, Action: ActAccountCreate, Target: a.Name, Detail: detail})
	return s.Get(a.ID)
}

// Update changes an account. An empty secret keeps the stored one; changed
// credentials are validated first.
func (s *AccountService) Update(ctx context.Context, actor Actor, id uint, in AccountInput) (*AccountView, error) {
	a, err := s.find(id)
	if err != nil {
		return nil, err
	}
	in.Provider = a.Provider
	if in.Partition == "" {
		in.Partition = a.Partition
	}
	if err := s.normalize(&in, false); err != nil {
		return nil, err
	}
	if in.AccessKeyID == "" {
		in.AccessKeyID = a.AccessKeyID
	}
	credChanged := in.AccessKeyID != a.AccessKeyID || in.AccessKeySecret != "" || in.RoleARN != a.RoleARN || in.Partition != a.Partition
	updates := map[string]any{}
	var changes []string
	if in.Name != a.Name {
		changes = append(changes, fmt.Sprintf("名称：%s → %s", a.Name, in.Name))
		updates["name"] = in.Name
	}
	if in.Remark != a.Remark {
		changes = append(changes, "备注已修改")
		updates["remark"] = in.Remark
	}
	if !slices.Equal(in.Regions, []string(a.Regions)) {
		changes = append(changes, fmt.Sprintf("同步地域：%s → %s", regionSummary(a.Regions, -1), regionSummary(in.Regions, -1)))
		updates["regions"] = model.StringList(in.Regions)
	}
	if in.Enabled != nil && *in.Enabled != a.Enabled {
		updates["enabled"] = *in.Enabled
		changes = append(changes, map[bool]string{true: "状态：已停用 → 启用", false: "状态：启用 → 已停用"}[*in.Enabled])
	}
	if credChanged {
		cred, err := s.Credential(a)
		if err != nil && !errors.Is(err, secret.ErrDecrypt) {
			return nil, err
		}
		cred.AccessKeyID, cred.RoleARN, cred.Partition, cred.CacheKey = in.AccessKeyID, in.RoleARN, in.Partition, ""
		if in.AccessKeySecret != "" {
			cred.AccessKeySecret = in.AccessKeySecret
		} else if errors.Is(err, secret.ErrDecrypt) {
			return nil, apperr.Invalid(secret.ErrDecrypt.Error())
		}
		p, err := s.Provider(a)
		if err != nil {
			return nil, err
		}
		res, err := s.check(ctx, p, cred)
		if err != nil {
			return nil, err
		}
		updates["cloud_account_uid"] = res.AccountUID
		if in.AccessKeyID != a.AccessKeyID {
			updates["access_key_id"] = in.AccessKeyID
			changes = append(changes, "AccessKey 已更换")
		}
		if in.AccessKeySecret != "" {
			enc, err := s.box.Encrypt(in.AccessKeySecret)
			if err != nil {
				return nil, err
			}
			updates["secret_enc"] = enc
			changes = append(changes, "Secret 已更换")
		}
		if in.RoleARN != a.RoleARN {
			updates["role_arn"] = in.RoleARN
			changes = append(changes, fmt.Sprintf("角色 ARN：%s → %s", orDash(a.RoleARN), orDash(in.RoleARN)))
		}
		if in.Partition != a.Partition {
			updates["partition"] = in.Partition
			changes = append(changes, fmt.Sprintf("分区：%s → %s", partitionLabel(a.Partition), partitionLabel(in.Partition)))
		}
	}
	if len(updates) > 0 {
		if err := s.db.Model(a).Updates(updates).Error; err != nil {
			return nil, err
		}
		s.audit.Record(actor, AuditEntry{Category: model.AuditAccount, Action: ActAccountUpdate, Target: a.Name, Detail: strings.Join(changes, "；")})
	}
	return s.Get(a.ID)
}

// SetEnabled toggles whether the account takes part in syncing.
func (s *AccountService) SetEnabled(actor Actor, id uint, enabled bool) (*AccountView, error) {
	a, err := s.find(id)
	if err != nil {
		return nil, err
	}
	if a.Enabled != enabled {
		if err := s.db.Model(a).Update("enabled", enabled).Error; err != nil {
			return nil, err
		}
		action := ActAccountDisable
		if enabled {
			action = ActAccountEnable
		}
		s.audit.Record(actor, AuditEntry{Category: model.AuditAccount, Action: action, Target: a.Name})
	}
	return s.Get(id)
}

// Delete removes the account with its resources, jobs and CPU history.
func (s *AccountService) Delete(actor Actor, id uint) error {
	a, err := s.find(id)
	if err != nil {
		return err
	}
	var resources, jobs int64
	err = s.db.Transaction(func(tx *gorm.DB) error {
		r := tx.Where("account_id = ?", id).Delete(&model.Resource{})
		if r.Error != nil {
			return r.Error
		}
		resources = r.RowsAffected
		j := tx.Where("account_id = ?", id).Delete(&model.SyncJob{})
		if j.Error != nil {
			return j.Error
		}
		jobs = j.RowsAffected
		if err := tx.Where("account_id = ?", id).Delete(&model.HostCPUHourly{}).Error; err != nil {
			return err
		}
		return tx.Delete(a).Error
	})
	if err != nil {
		return err
	}
	s.audit.Record(actor, AuditEntry{Category: model.AuditAccount, Action: ActAccountDelete, Target: a.Name,
		Detail: fmt.Sprintf("同时删除 %d 项资源和 %d 条同步记录", resources, jobs)})
	return nil
}

func regionSummary(regions []string, available int) string {
	if len(regions) == 0 {
		if available > 0 {
			return fmt.Sprintf("全部地域（%d 个）", available)
		}
		return "全部地域"
	}
	return fmt.Sprintf("%d 个地域", len(regions))
}

func roleKind(provider string) string {
	if provider == model.ProviderAWS {
		return "AssumeRole"
	}
	return "RAM 角色"
}

func partitionLabel(p string) string {
	if p == model.PartitionAWSCN {
		return "中国区"
	}
	return "全球区"
}
