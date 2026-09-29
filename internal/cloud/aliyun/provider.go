// Package aliyun implements cloud.Provider for Alibaba Cloud with the
// darabonba ("dara") generation of the official SDKs. Only read-only APIs are
// called.
package aliyun

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	alb "github.com/alibabacloud-go/alb-20200616/v2/client"
	cms "github.com/alibabacloud-go/cms-20190101/v10/client"
	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	rds "github.com/alibabacloud-go/rds-20140815/v16/client"
	slb "github.com/alibabacloud-go/slb-20140515/v4/client"
	sts "github.com/alibabacloud-go/sts-20150401/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	osscred "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	credential "github.com/aliyun/credentials-go/credentials"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// defaultRegion is used for account-level calls (STS, region list, OSS list).
const defaultRegion = "cn-hangzhou"

// Provider talks to Alibaba Cloud.
type Provider struct {
	mu    sync.Mutex
	roles map[string]credential.Credential
	now   func() time.Time
}

// New creates the Alibaba Cloud provider.
func New() *Provider {
	return &Provider{roles: map[string]credential.Credential{}, now: time.Now}
}

func (p *Provider) Name() string { return model.ProviderAliyun }

// RegionName implements cloud.RegionNamer.
func (p *Provider) RegionName(id string) string { return RegionName(id) }

func (p *Provider) ResourceTypes() []cloud.TypeSpec {
	return []cloud.TypeSpec{
		{Type: model.TypeVM},
		{Type: model.TypeRDS},
		{Type: model.TypeLB},
		{Type: model.TypeBucket, Global: true},
		{Type: model.TypeDisk},
		{Type: model.TypeEIP},
	}
}

func runtime() *dara.RuntimeOptions {
	return &dara.RuntimeOptions{
		Autoretry:      dara.Bool(true),
		MaxAttempts:    dara.Int(3),
		ConnectTimeout: dara.Int(5000),
		ReadTimeout:    dara.Int(30000),
	}
}

// roleCredential returns a cached RAM role credential (it refreshes its STS
// token on its own).
func (p *Provider) roleCredential(cred cloud.Credential) (credential.Credential, error) {
	key := cred.CacheKey
	if key == "" {
		key = cred.AccessKeyID + "|" + cred.AccessKeySecret + "|" + cred.RoleARN
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.roles[key]; ok {
		return c, nil
	}
	c, err := credential.NewCredential(&credential.Config{
		Type:                  dara.String("ram_role_arn"),
		AccessKeyId:           dara.String(cred.AccessKeyID),
		AccessKeySecret:       dara.String(cred.AccessKeySecret),
		RoleArn:               dara.String(cred.RoleARN),
		RoleSessionName:       dara.String("yunshu-readonly"),
		RoleSessionExpiration: dara.Int(3600),
	})
	if err != nil {
		return nil, err
	}
	if len(p.roles) > 256 {
		p.roles = map[string]credential.Credential{}
	}
	p.roles[key] = c
	return c, nil
}

func (p *Provider) apiConfig(cred cloud.Credential, region string) (*openapiutil.Config, error) {
	cfg := &openapiutil.Config{
		RegionId:       dara.String(region),
		ConnectTimeout: dara.Int(5000),
		ReadTimeout:    dara.Int(30000),
		UserAgent:      dara.String("yunshu"),
	}
	if cred.RoleARN != "" {
		c, err := p.roleCredential(cred)
		if err != nil {
			return nil, cloud.Wrap(cloud.ErrAuth, err)
		}
		cfg.Credential = c
	} else {
		cfg.AccessKeyId = dara.String(cred.AccessKeyID)
		cfg.AccessKeySecret = dara.String(cred.AccessKeySecret)
	}
	return cfg, nil
}

// Validate calls STS GetCallerIdentity.
func (p *Provider) Validate(ctx context.Context, cred cloud.Credential) (*cloud.AccountInfo, error) {
	cfg, err := p.apiConfig(cred, defaultRegion)
	if err != nil {
		return nil, err
	}
	cfg.Endpoint = dara.String("sts.aliyuncs.com")
	client, err := sts.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	type result struct {
		resp *sts.GetCallerIdentityResponse
		err  error
	}
	// The STS client has no context-aware method; bound it ourselves.
	ch := make(chan result, 1)
	go func() {
		resp, err := client.GetCallerIdentityWithOptions(runtime())
		ch <- result{resp, err}
	}()
	select {
	case <-ctx.Done():
		return nil, cloud.Wrap(cloud.ErrNetwork, ctx.Err())
	case r := <-ch:
		if r.err != nil {
			return nil, classify(r.err)
		}
		if r.resp == nil || r.resp.Body == nil {
			return nil, errors.New("STS 返回为空")
		}
		return &cloud.AccountInfo{AccountUID: dara.StringValue(r.resp.Body.AccountId), Arn: dara.StringValue(r.resp.Body.Arn)}, nil
	}
}

// ListRegions lists ECS regions with their Chinese names.
func (p *Provider) ListRegions(ctx context.Context, cred cloud.Credential) ([]cloud.Region, error) {
	client, err := p.ecs(cred, defaultRegion)
	if err != nil {
		return nil, err
	}
	resp, err := client.DescribeRegionsWithContext(ctx, &ecs.DescribeRegionsRequest{AcceptLanguage: dara.String("zh-CN")}, runtime())
	if err != nil {
		return nil, classify(err)
	}
	var out []cloud.Region
	if resp.Body != nil && resp.Body.Regions != nil {
		for _, r := range resp.Body.Regions.Region {
			id := dara.StringValue(r.RegionId)
			name := dara.StringValue(r.LocalName)
			if name == "" {
				name = RegionName(id)
			}
			out = append(out, cloud.Region{ID: id, Name: name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return regionOrder(out[i].ID) < regionOrder(out[j].ID) })
	return out, nil
}

func (p *Provider) ecs(cred cloud.Credential, region string) (*ecs.Client, error) {
	cfg, err := p.apiConfig(cred, region)
	if err != nil {
		return nil, err
	}
	return ecs.NewClient(cfg)
}

func (p *Provider) rds(cred cloud.Credential, region string) (*rds.Client, error) {
	cfg, err := p.apiConfig(cred, region)
	if err != nil {
		return nil, err
	}
	return rds.NewClient(cfg)
}

func (p *Provider) slb(cred cloud.Credential, region string) (*slb.Client, error) {
	cfg, err := p.apiConfig(cred, region)
	if err != nil {
		return nil, err
	}
	return slb.NewClient(cfg)
}

func (p *Provider) alb(cred cloud.Credential, region string) (*alb.Client, error) {
	cfg, err := p.apiConfig(cred, region)
	if err != nil {
		return nil, err
	}
	return alb.NewClient(cfg)
}

func (p *Provider) cms(cred cloud.Credential, region string) (*cms.Client, error) {
	cfg, err := p.apiConfig(cred, region)
	if err != nil {
		return nil, err
	}
	return cms.NewClient(cfg)
}

func (p *Provider) oss(cred cloud.Credential, region string) (*oss.Client, error) {
	var provider osscred.CredentialsProvider
	if cred.RoleARN != "" {
		c, err := p.roleCredential(cred)
		if err != nil {
			return nil, cloud.Wrap(cloud.ErrAuth, err)
		}
		provider = osscred.CredentialsProviderFunc(func(ctx context.Context) (osscred.Credentials, error) {
			m, err := c.GetCredential()
			if err != nil {
				return osscred.Credentials{}, err
			}
			return osscred.Credentials{
				AccessKeyID:     dara.StringValue(m.AccessKeyId),
				AccessKeySecret: dara.StringValue(m.AccessKeySecret),
				SecurityToken:   dara.StringValue(m.SecurityToken),
			}, nil
		})
	} else {
		provider = osscred.NewStaticCredentialsProvider(cred.AccessKeyID, cred.AccessKeySecret)
	}
	cfg := oss.LoadDefaultConfig().
		WithCredentialsProvider(provider).
		WithRegion(region).
		WithConnectTimeout(5 * time.Second).
		WithReadWriteTimeout(30 * time.Second).
		WithRetryMaxAttempts(3)
	return oss.NewClient(cfg), nil
}

// Collect lists one resource type in one region.
func (p *Provider) Collect(ctx context.Context, cred cloud.Credential, typ, region string) ([]cloud.Resource, error) {
	switch typ {
	case model.TypeVM:
		c, err := p.ecs(cred, region)
		if err != nil {
			return nil, err
		}
		return collectECS(ctx, c, region)
	case model.TypeRDS:
		c, err := p.rds(cred, region)
		if err != nil {
			return nil, err
		}
		return collectRDS(ctx, c, region)
	case model.TypeLB:
		clb, err := p.slb(cred, region)
		if err != nil {
			return nil, err
		}
		albc, err := p.alb(cred, region)
		if err != nil {
			return nil, err
		}
		return collectLB(ctx, clb, albc, region)
	case model.TypeBucket:
		lister, err := p.oss(cred, defaultRegion)
		if err != nil {
			return nil, err
		}
		return collectOSS(ctx, lister, func(r string) (bucketStater, error) { return p.oss(cred, r) })
	case model.TypeDisk:
		c, err := p.ecs(cred, region)
		if err != nil {
			return nil, err
		}
		return collectDisks(ctx, c, region)
	case model.TypeEIP:
		c, err := p.ecs(cred, region)
		if err != nil {
			return nil, err
		}
		return collectEIPs(ctx, c, region)
	}
	return nil, nil
}

// CPUSnapshot reads hourly CPU utilization of ECS instances from CloudMonitor.
func (p *Provider) CPUSnapshot(ctx context.Context, cred cloud.Credential, region string, ids []string) (map[string]cloud.CPUStat, error) {
	c, err := p.cms(cred, region)
	if err != nil {
		return nil, err
	}
	return cpuSnapshot(ctx, c, region, ids, p.now())
}

func (p *Provider) SupportedMetrics(ref cloud.ResourceRef) []string { return SupportedMetrics(ref) }

// QueryMetrics reads CloudMonitor.
func (p *Provider) QueryMetrics(ctx context.Context, cred cloud.Credential, ref cloud.ResourceRef, q cloud.MetricQuery) ([]cloud.Series, error) {
	c, err := p.cms(cred, ref.Region)
	if err != nil {
		return nil, err
	}
	return queryMetrics(ctx, c, ref, q)
}
