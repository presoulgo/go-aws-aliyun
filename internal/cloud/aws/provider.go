// Package aws implements cloud.Provider for Amazon Web Services using the
// AWS SDK for Go v2. Only read-only APIs are called.
package aws

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// Provider talks to AWS.
type Provider struct {
	mu         sync.Mutex
	sessions   map[string]aws.Config
	httpClient *awshttp.BuildableClient
	now        func() time.Time
}

// New creates the AWS provider.
func New() *Provider {
	return &Provider{
		sessions:   map[string]aws.Config{},
		httpClient: awshttp.NewBuildableClient().WithTimeout(60 * time.Second),
		now:        time.Now,
	}
}

func (p *Provider) Name() string { return model.ProviderAWS }

// ResourceTypes: EC2, RDS and ELBv2 are regional; S3 lists buckets globally.
func (p *Provider) ResourceTypes() []cloud.TypeSpec {
	return []cloud.TypeSpec{
		{Type: model.TypeVM},
		{Type: model.TypeRDS},
		{Type: model.TypeLB},
		{Type: model.TypeBucket, Global: true},
	}
}

func defaultRegion(partition string) string {
	if partition == model.PartitionAWSCN {
		return "cn-north-1"
	}
	return "us-east-1"
}

// config returns a cached aws.Config for the credential. It deliberately does
// not read shared config files or environment credentials of the host.
func (p *Provider) config(cred cloud.Credential) aws.Config {
	key := cred.CacheKey
	if key == "" {
		key = cred.Partition + "|" + cred.AccessKeyID + "|" + cred.AccessKeySecret + "|" + cred.RoleARN
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if cfg, ok := p.sessions[key]; ok {
		return cfg
	}
	cfg := aws.Config{
		Region:           defaultRegion(cred.Partition),
		Credentials:      aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(cred.AccessKeyID, cred.AccessKeySecret, "")),
		RetryMaxAttempts: 3,
		RetryMode:        aws.RetryModeStandard,
		HTTPClient:       p.httpClient,
		AppID:            "yunshu",
	}
	if cred.RoleARN != "" {
		stsClient := sts.NewFromConfig(cfg)
		cfg.Credentials = aws.NewCredentialsCache(stscreds.NewAssumeRoleProvider(stsClient, cred.RoleARN, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = "yunshu-readonly"
		}))
	}
	if len(p.sessions) > 256 {
		p.sessions = map[string]aws.Config{}
	}
	p.sessions[key] = cfg
	return cfg
}

func (p *Provider) ec2(cred cloud.Credential, region string) *ec2.Client {
	return ec2.NewFromConfig(p.config(cred), func(o *ec2.Options) { o.Region = region })
}

func (p *Provider) rds(cred cloud.Credential, region string) *rds.Client {
	return rds.NewFromConfig(p.config(cred), func(o *rds.Options) { o.Region = region })
}

func (p *Provider) elb(cred cloud.Credential, region string) *elbv2.Client {
	return elbv2.NewFromConfig(p.config(cred), func(o *elbv2.Options) { o.Region = region })
}

func (p *Provider) cloudwatch(cred cloud.Credential, region string) *cloudwatch.Client {
	return cloudwatch.NewFromConfig(p.config(cred), func(o *cloudwatch.Options) { o.Region = region })
}

func (p *Provider) s3(cred cloud.Credential) *s3.Client {
	return s3.NewFromConfig(p.config(cred), func(o *s3.Options) { o.Region = defaultRegion(cred.Partition) })
}

// Validate calls STS GetCallerIdentity.
func (p *Provider) Validate(ctx context.Context, cred cloud.Credential) (*cloud.AccountInfo, error) {
	out, err := sts.NewFromConfig(p.config(cred)).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, classify(err)
	}
	return &cloud.AccountInfo{AccountUID: aws.ToString(out.Account), Arn: aws.ToString(out.Arn)}, nil
}

// ListRegions returns the regions enabled for the account.
func (p *Provider) ListRegions(ctx context.Context, cred cloud.Credential) ([]cloud.Region, error) {
	out, err := p.ec2(cred, defaultRegion(cred.Partition)).DescribeRegions(ctx, &ec2.DescribeRegionsInput{AllRegions: aws.Bool(false)})
	if err != nil {
		return nil, classify(err)
	}
	regions := make([]cloud.Region, 0, len(out.Regions))
	for _, r := range out.Regions {
		id := aws.ToString(r.RegionName)
		regions = append(regions, cloud.Region{ID: id, Name: RegionName(id)})
	}
	sort.Slice(regions, func(i, j int) bool { return regionOrder(regions[i].ID) < regionOrder(regions[j].ID) })
	return regions, nil
}

// Collect lists one resource type in one region.
func (p *Provider) Collect(ctx context.Context, cred cloud.Credential, typ, region string) ([]cloud.Resource, error) {
	switch typ {
	case model.TypeVM:
		return collectEC2(ctx, p.ec2(cred, region), region)
	case model.TypeRDS:
		return collectRDS(ctx, p.rds(cred, region), region)
	case model.TypeLB:
		return collectELB(ctx, p.elb(cred, region), region)
	case model.TypeBucket:
		return collectS3(ctx, p.s3(cred), func(r string) cwAPI { return p.cloudwatch(cred, r) }, defaultRegion(cred.Partition), p.now())
	}
	return nil, nil
}

// CPUSnapshot returns hourly CPU utilization of EC2 instances.
func (p *Provider) CPUSnapshot(ctx context.Context, cred cloud.Credential, region string, ids []string) (map[string]cloud.CPUStat, error) {
	return cpuSnapshot(ctx, p.cloudwatch(cred, region), ids, p.now())
}

// SupportedMetrics reports the catalog metrics CloudWatch provides for ref.
func (p *Provider) SupportedMetrics(ref cloud.ResourceRef) []string { return SupportedMetrics(ref) }

// QueryMetrics reads CloudWatch.
func (p *Provider) QueryMetrics(ctx context.Context, cred cloud.Credential, ref cloud.ResourceRef, q cloud.MetricQuery) ([]cloud.Series, error) {
	return queryMetrics(ctx, p.cloudwatch(cred, ref.Region), ref, q)
}
