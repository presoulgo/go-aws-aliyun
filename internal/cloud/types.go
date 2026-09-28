// Package cloud defines the provider abstraction shared by AWS, Alibaba Cloud
// and the demo provider: normalized resources, regions and metric series.
package cloud

import (
	"context"
	"time"
)

// Credential is a decrypted cloud credential handed to a provider.
type Credential struct {
	AccessKeyID     string
	AccessKeySecret string
	// RoleARN, when set, is assumed with the access key (AWS AssumeRole or
	// Alibaba Cloud RAM role) and the resulting temporary credentials are used.
	RoleARN string
	// Partition is "aws" or "aws-cn" for AWS; unused for Alibaba Cloud.
	Partition string
	// CacheKey identifies this credential version so providers can reuse
	// sessions. It changes whenever the account is edited.
	CacheKey string
}

// AccountInfo identifies the cloud account behind a credential.
type AccountInfo struct {
	AccountUID string `json:"account_uid"`
	Arn        string `json:"arn"`
}

// Region is a cloud region available to the account.
type Region struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TypeSpec describes a resource type collected by a provider.
type TypeSpec struct {
	Type string
	// Global types are listed once per account; each item carries its own region.
	Global bool
}

// Resource is the normalized result of a collector.
type Resource struct {
	Type       string
	Region     string
	Zone       string
	ResourceID string
	Name       string
	Status     string
	RawStatus  string
	Spec       string
	PrivateIPs []string
	PublicIPs  []string
	VpcID      string
	ChargeType string
	ExpireAt   *time.Time
	CreatedAt  *time.Time
	Tags       map[string]string
	Extra      map[string]any
}

// CPUStat is the hourly CPU utilization of one host over the last day.
type CPUStat struct {
	// Hourly maps an hour start (unix seconds) to the average utilization.
	Hourly map[int64]float64
}

// Last returns the most recent hourly value.
func (c CPUStat) Last() (float64, bool) {
	var bestHour int64 = -1
	var v float64
	for h, val := range c.Hourly {
		if h > bestHour {
			bestHour, v = h, val
		}
	}
	return v, bestHour >= 0
}

// Average returns the mean of all hourly values.
func (c CPUStat) Average() (float64, bool) {
	if len(c.Hourly) == 0 {
		return 0, false
	}
	var sum float64
	for _, v := range c.Hourly {
		sum += v
	}
	return sum / float64(len(c.Hourly)), true
}

// ResourceRef carries what a provider needs to query metrics of a resource.
type ResourceRef struct {
	Type       string
	Region     string
	ResourceID string
	Name       string
	Extra      map[string]any
}

// MetricQuery selects metric keys over a time window.
type MetricQuery struct {
	Keys   []string
	Start  time.Time
	End    time.Time
	Period time.Duration
}

// Point is [unix milliseconds, value].
type Point [2]float64

// Series is one metric time series in the unit declared by the catalog.
type Series struct {
	Key    string  `json:"key"`
	Unit   string  `json:"unit"`
	Points []Point `json:"points"`
}

// Provider is implemented by each cloud.
type Provider interface {
	Name() string
	Validate(ctx context.Context, cred Credential) (*AccountInfo, error)
	ListRegions(ctx context.Context, cred Credential) ([]Region, error)
	ResourceTypes() []TypeSpec
	// Collect lists resources of one type in one region ("" for global types).
	Collect(ctx context.Context, cred Credential, typ, region string) ([]Resource, error)
	// CPUSnapshot returns hourly CPU utilization for the last 24 hours of the
	// given hosts in a region, keyed by resource id.
	CPUSnapshot(ctx context.Context, cred Credential, region string, ids []string) (map[string]CPUStat, error)
	// SupportedMetrics lists the catalog metric keys available for a resource.
	SupportedMetrics(ref ResourceRef) []string
	QueryMetrics(ctx context.Context, cred Credential, ref ResourceRef, q MetricQuery) ([]Series, error)
}
