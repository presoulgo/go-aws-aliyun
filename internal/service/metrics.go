package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"github.com/presoulgo/go-aws-aliyun/internal/apperr"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// MetricsService queries cloud monitoring on demand, with a short cache so
// repeated views do not exhaust provider API quotas.
type MetricsService struct {
	db       *gorm.DB
	accounts *AccountService
	registry *cloud.Registry
	ttl      time.Duration
	timeout  time.Duration
	now      func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	series  []cloud.Series
	expires time.Time
}

func NewMetricsService(db *gorm.DB, accounts *AccountService, registry *cloud.Registry, ttl time.Duration) *MetricsService {
	return &MetricsService{db: db, accounts: accounts, registry: registry, ttl: ttl, timeout: 30 * time.Second, now: time.Now, cache: map[string]cacheEntry{}}
}

// Catalog lists the standard metrics, optionally for one resource type.
func (s *MetricsService) Catalog(typ string) []cloud.MetricDef {
	if typ == "" {
		return cloud.Catalog
	}
	return cloud.MetricsFor(typ)
}

// MetricsResult is the metric data of one resource.
type MetricsResult struct {
	ResourceID uint           `json:"resource_id"`
	Range      string         `json:"range"`
	Period     int            `json:"period"`
	Start      time.Time      `json:"start"`
	End        time.Time      `json:"end"`
	Supported  []string       `json:"supported"`
	Series     []cloud.Series `json:"series"`
}

type target struct {
	res  model.Resource
	acc  model.CloudAccount
	prov cloud.Provider
	cred cloud.Credential
}

func (s *MetricsService) target(id uint) (*target, error) {
	var t target
	if err := s.db.First(&t.res, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("资源不存在")
		}
		return nil, err
	}
	if err := s.db.First(&t.acc, t.res.AccountID).Error; err != nil {
		return nil, apperr.NotFound("资源所属的云账号不存在")
	}
	prov, err := s.accounts.Provider(&t.acc)
	if err != nil {
		return nil, err
	}
	cred, err := s.accounts.Credential(&t.acc)
	if err != nil {
		return nil, err
	}
	t.prov, t.cred = prov, cred
	return &t, nil
}

func (s *MetricsService) window(rangeKey string) (cloud.RangeSpec, time.Time, time.Time, error) {
	if rangeKey == "" {
		rangeKey = "6h"
	}
	spec, ok := cloud.LookupRange(rangeKey)
	if !ok {
		return spec, time.Time{}, time.Time{}, apperr.Invalid("时间范围只支持 1h、6h、24h、7d")
	}
	end := s.now().UTC()
	return spec, end.Add(-spec.Duration), end, nil
}

// ForResource returns metric series of one resource.
func (s *MetricsService) ForResource(ctx context.Context, id uint, keys []string, rangeKey string) (*MetricsResult, error) {
	spec, start, end, err := s.window(rangeKey)
	if err != nil {
		return nil, err
	}
	t, err := s.target(id)
	if err != nil {
		return nil, err
	}
	supported := t.prov.SupportedMetrics(Ref(&t.res))
	if supported == nil {
		supported = []string{}
	}
	var want []string
	if len(keys) == 0 {
		want = supported
	} else {
		for _, k := range keys {
			if slices.Contains(supported, k) && !slices.Contains(want, k) {
				want = append(want, k)
			}
		}
	}
	res := &MetricsResult{ResourceID: id, Range: spec.Key, Period: int(spec.Period / time.Second), Start: start, End: end, Supported: supported, Series: []cloud.Series{}}
	if len(want) == 0 {
		return res, nil
	}
	series, err := s.query(ctx, t, want, spec, start, end)
	if err != nil {
		return nil, err
	}
	res.Series = series
	return res, nil
}

func (s *MetricsService) query(ctx context.Context, t *target, keys []string, spec cloud.RangeSpec, start, end time.Time) ([]cloud.Series, error) {
	key := fmt.Sprintf("%d|%d|%s|%s", t.res.ID, t.acc.UpdatedAt.UnixNano(), spec.Key, strings.Join(keys, ","))
	now := s.now()
	s.mu.Lock()
	if e, ok := s.cache[key]; ok && now.Before(e.expires) {
		s.mu.Unlock()
		return e.series, nil
	}
	s.mu.Unlock()

	qctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	series, err := t.prov.QueryMetrics(qctx, t.cred, Ref(&t.res), cloud.MetricQuery{Keys: keys, Start: start, End: end, Period: spec.Period})
	if err != nil {
		return nil, apperr.Upstream("查询监控数据失败：" + cloud.Describe(err)).Wrap(err)
	}
	for i := range series {
		if series[i].Points == nil {
			series[i].Points = []cloud.Point{}
		}
	}
	s.mu.Lock()
	if len(s.cache) > 500 {
		for k, e := range s.cache {
			if now.After(e.expires) {
				delete(s.cache, k)
			}
		}
		if len(s.cache) > 500 {
			s.cache = map[string]cacheEntry{}
		}
	}
	s.cache[key] = cacheEntry{series: series, expires: now.Add(s.ttl)}
	s.mu.Unlock()
	return series, nil
}

// CompareInput selects resources to compare on one metric.
type CompareInput struct {
	ResourceIDs []uint `json:"resource_ids"`
	Key         string `json:"key"`
	Range       string `json:"range"`
}

// CompareSeries is one resource's series in a comparison.
type CompareSeries struct {
	ResourceID  uint          `json:"resource_id"`
	Name        string        `json:"name"`
	Provider    string        `json:"provider"`
	Region      string        `json:"region"`
	AccountName string        `json:"account_name"`
	Supported   bool          `json:"supported"`
	Unit        string        `json:"unit"`
	Points      []cloud.Point `json:"points"`
	Error       string        `json:"error,omitempty"`
}

// CompareResult is the response of a comparison query.
type CompareResult struct {
	Key    string          `json:"key"`
	Range  string          `json:"range"`
	Period int             `json:"period"`
	Items  []CompareSeries `json:"items"`
}

// Compare queries one metric for up to four resources of the same type.
func (s *MetricsService) Compare(ctx context.Context, in CompareInput) (*CompareResult, error) {
	if len(in.ResourceIDs) == 0 {
		return nil, apperr.Invalid("请至少选择一个资源")
	}
	if len(in.ResourceIDs) > 4 {
		return nil, apperr.Invalid("最多同时对比 4 个资源")
	}
	spec, start, end, err := s.window(in.Range)
	if err != nil {
		return nil, err
	}
	out := &CompareResult{Key: in.Key, Range: spec.Key, Period: int(spec.Period / time.Second), Items: make([]CompareSeries, len(in.ResourceIDs))}
	var typ string
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	var mu sync.Mutex
	for i, id := range in.ResourceIDs {
		t, err := s.target(id)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		if typ == "" {
			typ = t.res.Type
		} else if typ != t.res.Type {
			mu.Unlock()
			return nil, apperr.Invalid("只能对比同一类型的资源")
		}
		mu.Unlock()
		def, ok := cloud.LookupMetric(t.res.Type, in.Key)
		if !ok {
			return nil, apperr.Invalid("该资源类型没有这个指标")
		}
		item := CompareSeries{
			ResourceID: id, Name: t.res.Name, Provider: t.res.Provider, Region: t.res.Region,
			AccountName: t.acc.Name, Unit: def.Unit, Points: []cloud.Point{},
		}
		item.Supported = slices.Contains(t.prov.SupportedMetrics(Ref(&t.res)), in.Key)
		out.Items[i] = item
		if !item.Supported {
			continue
		}
		g.Go(func() error {
			series, err := s.query(gctx, t, []string{in.Key}, spec, start, end)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if e, ok := apperr.As(err); ok {
					out.Items[i].Error = e.Msg
				} else {
					out.Items[i].Error = err.Error()
				}
				return nil
			}
			if len(series) > 0 {
				out.Items[i].Points = series[0].Points
			}
			return nil
		})
	}
	_ = g.Wait()
	return out, nil
}
