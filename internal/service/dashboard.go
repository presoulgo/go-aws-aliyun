package service

import (
	"math"
	"time"

	"gorm.io/gorm"

	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// DashboardService computes the overview page.
type DashboardService struct {
	db      *gorm.DB
	idleCPU float64
	expDays int
	now     func() time.Time
}

func NewDashboardService(db *gorm.DB, idleCPU float64, expiringDays int) *DashboardService {
	return &DashboardService{db: db, idleCPU: idleCPU, expDays: expiringDays, now: time.Now}
}

// TypeCount is the number of resources of one type per cloud.
type TypeCount struct {
	Type   string `json:"type"`
	AWS    int64  `json:"aws"`
	Aliyun int64  `json:"aliyun"`
}

// TrendPoint is the fleet CPU average of one hour per cloud.
type TrendPoint struct {
	Time   int64    `json:"t"`
	AWS    *float64 `json:"aws"`
	Aliyun *float64 `json:"aliyun"`
}

// TopItem is a busy host.
type TopItem struct {
	ID          uint    `json:"id"`
	Name        string  `json:"name"`
	Provider    string  `json:"provider"`
	Region      string  `json:"region"`
	AccountName string  `json:"account_name"`
	CPU         float64 `json:"cpu"`
}

// ExpireItem is a prepaid resource that expires soon.
type ExpireItem struct {
	ID       uint      `json:"id"`
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Provider string    `json:"provider"`
	Region   string    `json:"region"`
	ExpireAt time.Time `json:"expire_at"`
	Days     int       `json:"days"`
}

// SyncItem is the latest sync of an account.
type SyncItem struct {
	AccountID   uint       `json:"account_id"`
	AccountName string     `json:"account_name"`
	Provider    string     `json:"provider"`
	JobID       uint       `json:"job_id"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Items       int        `json:"items"`
	Regions     int64      `json:"regions"`
	ErrorCount  int        `json:"error_count"`
	Message     string     `json:"message"`
	TasksTotal  int        `json:"tasks_total"`
	TasksDone   int        `json:"tasks_done"`
	// FirstError is the first failed task, shown as the job summary.
	FirstError *model.TaskError `json:"first_error,omitempty"`
}

// Summary is the overview page data.
type Summary struct {
	Provider         string           `json:"provider"`
	Accounts         int64            `json:"accounts"`
	AccountsByCloud  map[string]int64 `json:"accounts_by_cloud"`
	AccountNames     []string         `json:"account_names"`
	Resources        int64            `json:"resources"`
	Regions          int64            `json:"regions"`
	VMTotal          int64            `json:"vm_total"`
	VMRunning        int64            `json:"vm_running"`
	Idle             int64            `json:"idle"`
	IdleThreshold    float64          `json:"idle_threshold"`
	Expiring         int64            `json:"expiring"`
	ExpiringDays     int              `json:"expiring_days"`
	NextExpireInDays *int             `json:"next_expire_in_days"`
	Distribution     []TypeCount      `json:"distribution"`
	CPUTrend         []TrendPoint     `json:"cpu_trend"`
	TopCPU           []TopItem        `json:"top_cpu"`
	ExpiringItems    []ExpireItem     `json:"expiring_items"`
	RecentSync       []SyncItem       `json:"recent_sync"`
}

// Summary builds the overview, optionally limited to one cloud.
func (s *DashboardService) Summary(provider string) (*Summary, error) {
	if provider == "all" {
		provider = ""
	}
	now := s.now().UTC()
	out := &Summary{Provider: provider, AccountsByCloud: map[string]int64{}, IdleThreshold: s.idleCPU, ExpiringDays: s.expDays}
	scope := func(q *gorm.DB, col string) *gorm.DB {
		if provider != "" {
			return q.Where(col+" = ?", provider)
		}
		return q
	}

	var accounts []model.CloudAccount
	if err := scope(s.db.Model(&model.CloudAccount{}), "provider").Order("id").Find(&accounts).Error; err != nil {
		return nil, err
	}
	for _, a := range accounts {
		out.Accounts++
		out.AccountsByCloud[a.Provider]++
		out.AccountNames = append(out.AccountNames, a.Name)
	}

	res := func() *gorm.DB { return scope(s.db.Model(&model.Resource{}), "provider") }
	if err := res().Count(&out.Resources).Error; err != nil {
		return nil, err
	}
	if err := res().Select("COUNT(DISTINCT provider || ':' || region)").Scan(&out.Regions).Error; err != nil {
		return nil, err
	}
	if err := res().Where("type = ?", model.TypeVM).Count(&out.VMTotal).Error; err != nil {
		return nil, err
	}
	if err := res().Where("type = ? AND status = ?", model.TypeVM, model.StatusRunning).Count(&out.VMRunning).Error; err != nil {
		return nil, err
	}
	if err := res().Where("type = ? AND status = ? AND cpu_24h IS NOT NULL AND cpu_24h < ?", model.TypeVM, model.StatusRunning, s.idleCPU).Count(&out.Idle).Error; err != nil {
		return nil, err
	}
	horizon := now.Add(time.Duration(s.expDays) * 24 * time.Hour)
	if err := res().Where("expire_at IS NOT NULL AND expire_at >= ? AND expire_at <= ?", now, horizon).Count(&out.Expiring).Error; err != nil {
		return nil, err
	}

	type dist struct {
		Type     string
		Provider string
		N        int64
	}
	var rows []dist
	if err := res().Select("type, provider, COUNT(*) AS n").Group("type, provider").Scan(&rows).Error; err != nil {
		return nil, err
	}
	byType := map[string]*TypeCount{}
	for _, t := range []string{model.TypeVM, model.TypeRDS, model.TypeLB, model.TypeBucket} {
		tc := &TypeCount{Type: t}
		byType[t] = tc
	}
	for _, r := range rows {
		tc, ok := byType[r.Type]
		if !ok {
			continue
		}
		if r.Provider == model.ProviderAWS {
			tc.AWS = r.N
		} else if r.Provider == model.ProviderAliyun {
			tc.Aliyun = r.N
		}
	}
	for _, t := range []string{model.TypeVM, model.TypeRDS, model.TypeLB, model.TypeBucket} {
		out.Distribution = append(out.Distribution, *byType[t])
	}

	trend, err := s.cpuTrend(provider, now)
	if err != nil {
		return nil, err
	}
	out.CPUTrend = trend

	var top []TopItem
	err = scope(s.db.Model(&model.Resource{}), "resources.provider").
		Select("resources.id, resources.name, resources.provider, resources.region, resources.cpu_1h AS cpu, cloud_accounts.name AS account_name").
		Joins("LEFT JOIN cloud_accounts ON cloud_accounts.id = resources.account_id").
		Where("resources.type = ? AND resources.status = ? AND resources.cpu_1h IS NOT NULL", model.TypeVM, model.StatusRunning).
		Order("resources.cpu_1h DESC").Limit(5).Scan(&top).Error
	if err != nil {
		return nil, err
	}
	out.TopCPU = top
	if out.TopCPU == nil {
		out.TopCPU = []TopItem{}
	}

	var exp []model.Resource
	if err := res().Where("expire_at IS NOT NULL AND expire_at >= ? AND expire_at <= ?", now, horizon).Order("expire_at ASC").Limit(4).Find(&exp).Error; err != nil {
		return nil, err
	}
	out.ExpiringItems = []ExpireItem{}
	for _, r := range exp {
		days := int(math.Ceil(r.ExpireAt.Sub(now).Hours() / 24))
		out.ExpiringItems = append(out.ExpiringItems, ExpireItem{ID: r.ID, Name: r.Name, Type: r.Type, Provider: r.Provider, Region: r.Region, ExpireAt: *r.ExpireAt, Days: days})
	}
	if len(out.ExpiringItems) > 0 {
		d := out.ExpiringItems[0].Days
		out.NextExpireInDays = &d
	}

	recent, err := s.recentSync(accounts)
	if err != nil {
		return nil, err
	}
	out.RecentSync = recent
	return out, nil
}

func (s *DashboardService) cpuTrend(provider string, now time.Time) ([]TrendPoint, error) {
	last := now.Truncate(time.Hour)
	first := last.Add(-23 * time.Hour)
	type row struct {
		Provider string
		Hour     int64
		S        float64
		C        int64
	}
	var rows []row
	q := s.db.Model(&model.HostCPUHourly{}).Select("provider, hour, SUM(sum) AS s, SUM(count) AS c").
		Where("hour >= ? AND hour <= ?", first.Unix(), last.Unix()).Group("provider, hour")
	if provider != "" {
		q = q.Where("provider = ?", provider)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	values := map[int64]*TrendPoint{}
	points := make([]TrendPoint, 0, 24)
	for h := first; !h.After(last); h = h.Add(time.Hour) {
		points = append(points, TrendPoint{Time: h.UnixMilli()})
	}
	for i := range points {
		values[points[i].Time/1000] = &points[i]
	}
	for _, r := range rows {
		p, ok := values[r.Hour]
		if !ok || r.C == 0 {
			continue
		}
		v := math.Round(r.S/float64(r.C)*10) / 10
		switch r.Provider {
		case model.ProviderAWS:
			p.AWS = &v
		case model.ProviderAliyun:
			p.Aliyun = &v
		}
	}
	return points, nil
}

func (s *DashboardService) recentSync(accounts []model.CloudAccount) ([]SyncItem, error) {
	out := []SyncItem{}
	if len(accounts) == 0 {
		return out, nil
	}
	ids := make([]uint, 0, len(accounts))
	names := map[uint]model.CloudAccount{}
	for _, a := range accounts {
		ids = append(ids, a.ID)
		names[a.ID] = a
	}
	var jobs []model.SyncJob
	sub := s.db.Model(&model.SyncJob{}).Select("MAX(id)").Where("account_id IN ?", ids).Group("account_id")
	if err := s.db.Where("id IN (?)", sub).Order("COALESCE(finished_at, started_at) DESC").Limit(4).Find(&jobs).Error; err != nil {
		return nil, err
	}
	type rc struct {
		AccountID uint
		R         int64
	}
	var regions []rc
	s.db.Model(&model.Resource{}).Select("account_id, COUNT(DISTINCT region) AS r").Where("account_id IN ?", ids).Group("account_id").Scan(&regions)
	regionCount := map[uint]int64{}
	for _, r := range regions {
		regionCount[r.AccountID] = r.R
	}
	for _, j := range jobs {
		items := 0
		for _, n := range j.Stats {
			items += n
		}
		a := names[j.AccountID]
		item := SyncItem{
			AccountID: j.AccountID, AccountName: a.Name, Provider: a.Provider, JobID: j.ID, Status: j.Status,
			StartedAt: j.StartedAt, FinishedAt: j.FinishedAt, Items: items, Regions: regionCount[j.AccountID],
			ErrorCount: j.ErrorCount, Message: j.Message, TasksTotal: j.TasksTotal, TasksDone: j.TasksDone,
		}
		if len(j.Errors) > 0 {
			e := j.Errors[0]
			item.FirstError = &e
		}
		out = append(out, item)
	}
	return out, nil
}
