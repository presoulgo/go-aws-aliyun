package service

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/presoulgo/go-aws-aliyun/internal/apperr"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// ResourceService queries collected resources.
type ResourceService struct {
	db        *gorm.DB
	registry  *cloud.Registry
	idleCPU   float64
	expDays   int
	now       func() time.Time
	freshness time.Duration
}

func (s *ResourceService) SetSyncInterval(interval time.Duration) {
	s.freshness = freshnessWindow(interval)
}

func (s *ResourceService) dataWindow() time.Duration {
	if s.freshness > 0 {
		return s.freshness
	}
	return freshnessWindow(0)
}

func NewResourceService(db *gorm.DB, registry *cloud.Registry, idleCPU float64, expiringDays int) *ResourceService {
	return &ResourceService{db: db, registry: registry, idleCPU: idleCPU, expDays: expiringDays, now: time.Now}
}

// ResourceFilter selects resources.
type ResourceFilter struct {
	Type      string
	Provider  string
	AccountID uint
	Region    string
	Status    string
	Query     string
	Idle      bool
	Expiring  bool
	Sort      string
	Page
}

// ResourceView is a resource with its account.
type ResourceView struct {
	model.Resource
	AccountName  string `json:"account_name"`
	Idle         bool   `json:"idle"`
	MetricsStale bool   `json:"metrics_stale"`
	DataStale    bool   `json:"data_stale"`
}

var tagKeyRe = regexp.MustCompile(`^[A-Za-z0-9_.:/=+\-@ ]{1,128}$`)

func (s *ResourceService) base(f ResourceFilter, withType bool) (*gorm.DB, error) {
	q := s.db.Model(&model.Resource{})
	if withType && f.Type != "" {
		q = q.Where("resources.type = ?", f.Type)
	}
	if f.Provider != "" && f.Provider != "all" {
		q = q.Where("resources.provider = ?", f.Provider)
	}
	if f.AccountID > 0 {
		q = q.Where("resources.account_id = ?", f.AccountID)
	}
	if f.Region != "" {
		q = q.Where("resources.region = ?", f.Region)
	}
	if f.Status != "" {
		q = q.Where("resources.status = ?", f.Status)
	}
	if f.Idle {
		q = q.Where("resources.type = ? AND resources.status = ? AND resources.cpu_24h IS NOT NULL AND resources.cpu_24h < ?", model.TypeVM, model.StatusRunning, s.idleCPU)
		q = withFreshCPU(q, s.now().UTC(), s.dataWindow())
	}
	if f.Expiring {
		now := s.now().UTC()
		q = q.Where("resources.expire_at IS NOT NULL AND resources.expire_at >= ? AND resources.expire_at <= ?", now, now.Add(time.Duration(s.expDays)*24*time.Hour))
	}
	for _, tok := range strings.Fields(f.Query) {
		if k, v, ok := strings.Cut(tok, ":"); ok && k != "" && tagKeyRe.MatchString(k) {
			// Tag filter "key:value" (value may be empty to match the key).
			path := `$."` + strings.ReplaceAll(k, `"`, ``) + `"`
			if v == "" {
				q = q.Where("json_extract(resources.tags, ?) IS NOT NULL", path)
			} else {
				q = q.Where("json_extract(resources.tags, ?) = ?", path, v)
			}
			continue
		}
		p := likePattern(tok)
		q = q.Where(`(resources.name LIKE ? ESCAPE '\' OR resources.resource_id LIKE ? ESCAPE '\' OR resources.private_ip LIKE ? ESCAPE '\' OR resources.public_ip LIKE ? ESCAPE '\')`, p, p, p, p)
	}
	return q, nil
}

var sortColumns = map[string]string{
	"cpu_1h":     "resources.cpu_1h",
	"cpu_24h":    "resources.cpu_24h",
	"name":       "resources.name",
	"expire_at":  "resources.expire_at",
	"created_at": "resources.cloud_created_at",
	"region":     "resources.region",
}

// List returns matching resources.
func (s *ResourceService) List(f ResourceFilter) ([]ResourceView, int64, error) {
	q, err := s.base(f, true)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := "resources.provider ASC, resources.region ASC, resources.name ASC"
	if f.Sort != "" {
		field, dir, _ := strings.Cut(f.Sort, ":")
		col, ok := sortColumns[field]
		if !ok {
			return nil, 0, apperr.Invalid("不支持的排序字段")
		}
		if dir != "asc" {
			dir = "desc"
		}
		// Empty values always sort last.
		order = col + " IS NULL, " + col + " " + strings.ToUpper(dir) + ", resources.name ASC"
	}
	p := f.Page.normalize(20, 200)
	var rows []ResourceView
	err = q.Select("resources.*, cloud_accounts.name AS account_name").
		Joins("LEFT JOIN cloud_accounts ON cloud_accounts.id = resources.account_id").
		Order(order).Offset(p.offset()).Limit(p.PageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	if err := s.freshnessFlags(rows); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *ResourceService) isIdle(r *model.Resource) bool {
	return r.Type == model.TypeVM && r.Status == model.StatusRunning && r.CPU24h != nil && *r.CPU24h < s.idleCPU && freshCPUAt(r.MetricsAt, s.now(), s.dataWindow()) && freshAt(&r.SyncedAt, s.now(), s.dataWindow())
}

func (s *ResourceService) freshnessFlags(rows []ResourceView) error {
	if len(rows) == 0 {
		return nil
	}
	ids := []uint{}
	seen := map[uint]bool{}
	for _, r := range rows {
		if !seen[r.AccountID] {
			ids = append(ids, r.AccountID)
			seen[r.AccountID] = true
		}
	}
	type key struct {
		account     uint
		typ, region string
	}
	failed := map[key]bool{}
	var scopes []model.SyncScope
	if err := s.db.Where("account_id IN ? AND status = ?", ids, model.JobFailed).Find(&scopes).Error; err != nil {
		return err
	}
	for _, scope := range scopes {
		failed[key{scope.AccountID, scope.Type, scope.Region}] = true
	}
	var accounts []model.CloudAccount
	if err := s.db.Select("id, last_sync_status").Where("id IN ?", ids).Find(&accounts).Error; err != nil {
		return err
	}
	failedAccounts := map[uint]bool{}
	for _, acc := range accounts {
		failedAccounts[acc.ID] = acc.LastSyncStatus == model.JobFailed
	}
	now := s.now()
	for i := range rows {
		r := &rows[i]
		r.DataStale = !freshAt(&r.SyncedAt, now, s.dataWindow()) || failedAccounts[r.AccountID] || failed[key{r.AccountID, r.Type, r.Region}] || failed[key{r.AccountID, r.Type, ""}]
		r.MetricsStale = r.Type == model.TypeVM && (r.DataStale || !freshCPUAt(r.MetricsAt, now, s.dataWindow()) || failed[key{r.AccountID, "metrics", r.Region}])
		r.Idle = s.isIdle(&r.Resource) && !r.MetricsStale && !r.DataStale
	}
	return nil
}

// Option is a value offered in a filter dropdown.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
	Count int64  `json:"count"`
}

// AccountOption is an account offered in the filter dropdown.
type AccountOption struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// Filters describes the choices of the resource filter bar.
type Filters struct {
	Counts   map[string]int64 `json:"counts"`
	Regions  []Option         `json:"regions"`
	Statuses []Option         `json:"statuses"`
	Accounts []AccountOption  `json:"accounts"`
}

// Filters returns per-type counts (respecting provider/account/query) and
// the regions and statuses present for the selected type.
func (s *ResourceService) Filters(f ResourceFilter) (*Filters, error) {
	out := &Filters{Counts: map[string]int64{model.TypeVM: 0, model.TypeRDS: 0, model.TypeLB: 0, model.TypeBucket: 0}}
	typeless := f
	typeless.Region, typeless.Status = "", ""
	q, err := s.base(typeless, false)
	if err != nil {
		return nil, err
	}
	type kv struct {
		K string
		N int64
	}
	var byType []kv
	if err := q.Select("resources.type AS k, COUNT(*) AS n").Group("resources.type").Scan(&byType).Error; err != nil {
		return nil, err
	}
	for _, r := range byType {
		out.Counts[r.K] = r.N
	}
	scoped := f
	scoped.Region, scoped.Status = "", ""
	type pr struct {
		P, K string
		N    int64
	}
	var byRegion []pr
	var statuses []kv
	q, _ = s.base(scoped, true)
	if err := q.Select("resources.provider AS p, resources.region AS k, COUNT(*) AS n").
		Group("resources.provider, resources.region").Scan(&byRegion).Error; err != nil {
		return nil, err
	}
	q, _ = s.base(scoped, true)
	if err := q.Select("resources.status AS k, COUNT(*) AS n").Group("resources.status").Scan(&statuses).Error; err != nil {
		return nil, err
	}
	// The same region ID can exist in both clouds (and even name different
	// places, e.g. eu-west-1), so merge counts and keep every distinct name.
	regionIdx := map[string]int{}
	for _, r := range byRegion {
		i, ok := regionIdx[r.K]
		if !ok {
			i = len(out.Regions)
			regionIdx[r.K] = i
			out.Regions = append(out.Regions, Option{Value: r.K})
		}
		out.Regions[i].Count += r.N
		if name := s.registry.RegionName(r.P, r.K); name != "" && !strings.Contains(out.Regions[i].Label, name) {
			if out.Regions[i].Label != "" {
				out.Regions[i].Label += " / "
			}
			out.Regions[i].Label += name
		}
	}
	sort.SliceStable(out.Regions, func(i, j int) bool {
		if out.Regions[i].Count != out.Regions[j].Count {
			return out.Regions[i].Count > out.Regions[j].Count
		}
		return out.Regions[i].Value < out.Regions[j].Value
	})
	for _, r := range statuses {
		out.Statuses = append(out.Statuses, Option{Value: r.K, Count: r.N})
	}
	var accounts []model.CloudAccount
	if err := s.db.Order("CASE provider WHEN 'aws' THEN 0 WHEN 'aliyun' THEN 1 ELSE 2 END, id").Find(&accounts).Error; err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if f.Provider == "" || f.Provider == "all" || a.Provider == f.Provider {
			out.Accounts = append(out.Accounts, AccountOption{ID: a.ID, Name: a.Name, Provider: a.Provider})
		}
	}
	if out.Regions == nil {
		out.Regions = []Option{}
	}
	if out.Statuses == nil {
		out.Statuses = []Option{}
	}
	return out, nil
}

// ResourceDetail is a resource with account info and available metrics.
type ResourceDetail struct {
	ResourceView
	CloudAccountUID  string   `json:"cloud_account_uid"`
	SupportedMetrics []string `json:"supported_metrics"`
}

// Get returns one resource.
func (s *ResourceService) Get(id uint) (*ResourceDetail, error) {
	var r model.Resource
	if err := s.db.First(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("资源不存在，可能已在最近一次同步中被删除")
		}
		return nil, err
	}
	var acc model.CloudAccount
	s.db.First(&acc, r.AccountID)
	d := &ResourceDetail{
		ResourceView:     ResourceView{Resource: r, AccountName: acc.Name, Idle: s.isIdle(&r)},
		CloudAccountUID:  acc.CloudAccountUID,
		SupportedMetrics: []string{},
	}
	views := []ResourceView{d.ResourceView}
	if err := s.freshnessFlags(views); err != nil {
		return nil, err
	}
	d.ResourceView = views[0]
	if p, ok := s.registry.Get(r.Provider); ok {
		if m := p.SupportedMetrics(Ref(&r)); m != nil {
			d.SupportedMetrics = m
		}
	}
	return d, nil
}

// Ref converts a stored resource to a provider reference.
func Ref(r *model.Resource) cloud.ResourceRef {
	return cloud.ResourceRef{Type: r.Type, Region: r.Region, ResourceID: r.ResourceID, Name: r.Name, Extra: r.Extra}
}
