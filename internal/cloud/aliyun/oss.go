package aliyun

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type bucketLister interface {
	NewListBucketsPaginator(req *oss.ListBucketsRequest, optFns ...func(*oss.PaginatorOptions)) *oss.ListBucketsPaginator
}

type bucketStater interface {
	GetBucketStat(ctx context.Context, req *oss.GetBucketStatRequest, optFns ...func(*oss.Options)) (*oss.GetBucketStatResult, error)
}

var ossClasses = map[string]string{
	"Standard":        "标准存储",
	"IA":              "低频访问",
	"Archive":         "归档存储",
	"ColdArchive":     "冷归档",
	"DeepColdArchive": "深度冷归档",
}

func collectOSS(ctx context.Context, lister bucketLister, statFor func(region string) (bucketStater, error)) ([]cloud.Resource, error) {
	var out []cloud.Resource
	pager := lister.NewListBucketsPaginator(&oss.ListBucketsRequest{})
	for pager.HasNext() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, classifyOSS(err)
		}
		for _, b := range page.Buckets {
			name := deref(b.Name)
			region := deref(b.Region)
			if region == "" {
				region = strings.TrimPrefix(deref(b.Location), "oss-")
			}
			class := deref(b.StorageClass)
			extra := map[string]any{"storage_class": class}
			if label, ok := ossClasses[class]; ok {
				extra["storage_class_label"] = label
			}
			out = append(out, cloud.Resource{
				Type:       model.TypeBucket,
				Region:     region,
				ResourceID: name,
				Name:       name,
				Status:     model.StatusRunning,
				RawStatus:  "available",
				Spec:       "OSS",
				ChargeType: model.ChargePostpaid,
				CreatedAt:  b.CreationDate,
				Tags:       map[string]string{},
				Extra:      extra,
			})
		}
	}

	// Bucket size and object count, a few buckets at a time.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range out {
		wg.Add(1)
		go func(r *cloud.Resource) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			client, err := statFor(r.Region)
			if err != nil {
				return
			}
			st, err := client.GetBucketStat(ctx, &oss.GetBucketStatRequest{Bucket: oss.Ptr(r.Name)})
			if err != nil {
				slog.Debug("查询 OSS 容量失败", "bucket", r.Name, "err", err)
				return
			}
			r.Extra["size_bytes"] = st.Storage
			r.Extra["object_count"] = st.ObjectCount
		}(&out[i])
	}
	wg.Wait()
	return out, nil
}

func classifyOSS(err error) error {
	var se *oss.ServiceError
	if errors.As(err, &se) {
		switch se.Code {
		case "InvalidAccessKeyId", "SignatureDoesNotMatch", "InvalidSecurityToken", "SecurityTokenExpired":
			return cloud.Wrap(cloud.ErrAuth, err)
		case "AccessDenied", "UserDisable":
			return cloud.Wrap(cloud.ErrPermission, err)
		}
	}
	return classify(err)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
