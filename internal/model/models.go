// Package model holds the GORM models persisted in the local database.
package model

import "time"

const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

// User is a platform user.
type User struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Username     string     `gorm:"size:64;uniqueIndex;not null" json:"username"`
	DisplayName  string     `gorm:"size:128" json:"display_name"`
	PasswordHash string     `gorm:"size:255;not null" json:"-"`
	Role         string     `gorm:"size:16;not null;default:viewer" json:"role"`
	Disabled     bool       `gorm:"not null;default:false" json:"disabled"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	LastLoginIP  string     `gorm:"size:64" json:"last_login_ip"`
	// TokenVersion is embedded in issued tokens. Bumping it (password change,
	// role change, disable) invalidates every token issued before.
	TokenVersion int       `gorm:"not null;default:0" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

// Cloud providers.
const (
	ProviderAWS    = "aws"
	ProviderAliyun = "aliyun"
)

// AWS partitions.
const (
	PartitionAWS   = "aws"
	PartitionAWSCN = "aws-cn"
)

// CloudAccount is one managed cloud account and its (encrypted) credentials.
type CloudAccount struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"size:128;not null"`
	Provider    string `gorm:"size:16;not null;index"`
	Partition   string `gorm:"size:16"`
	AccessKeyID string `gorm:"size:128;not null"`
	SecretEnc   string `gorm:"size:2048;not null"`
	RoleARN     string `gorm:"size:512"`
	// Regions limits collection; empty means every enabled region.
	Regions         StringList `gorm:"not null"`
	Enabled         bool       `gorm:"not null;default:true"`
	Remark          string     `gorm:"size:512"`
	CloudAccountUID string     `gorm:"size:64"`
	LastSyncAt      *time.Time
	LastSyncStatus  string `gorm:"size:16"`
	LastSyncError   string `gorm:"type:text"`
	LastJobID       uint
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Resource types.
const (
	TypeVM     = "vm"
	TypeRDS    = "rds"
	TypeLB     = "lb"
	TypeBucket = "bucket"
	// TypeDisk and TypeEIP only hold idle items (unattached disks, unbound
	// elastic IPs): anything collected under them is waste.
	TypeDisk = "disk"
	TypeEIP  = "eip"
)

// CoreTypes are the inventory types counted as "resources" on the dashboard.
var CoreTypes = []string{TypeVM, TypeRDS, TypeLB, TypeBucket}

// WasteTypes are the types whose every item is an idle, billable resource.
var WasteTypes = []string{TypeDisk, TypeEIP}

// Normalized resource statuses.
const (
	StatusRunning     = "running"
	StatusStopped     = "stopped"
	StatusPending     = "pending"
	StatusStarting    = "starting"
	StatusStopping    = "stopping"
	StatusChanging    = "changing"
	StatusTerminating = "terminating"
	StatusFailed      = "failed"
	StatusUnknown     = "unknown"
	// StatusAvailable is an unattached disk or unbound elastic IP.
	StatusAvailable = "available"
)

// Charge types.
const (
	ChargePrepaid  = "prepaid"
	ChargePostpaid = "postpaid"
)

// Resource is a cloud resource collected by a sync job.
type Resource struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	AccountID      uint       `gorm:"not null;uniqueIndex:uk_resource,priority:1;index" json:"account_id"`
	Provider       string     `gorm:"size:16;not null;index" json:"provider"`
	Type           string     `gorm:"size:16;not null;uniqueIndex:uk_resource,priority:2;index" json:"type"`
	Region         string     `gorm:"size:64;not null;uniqueIndex:uk_resource,priority:3;index" json:"region"`
	ResourceID     string     `gorm:"size:255;not null;uniqueIndex:uk_resource,priority:4" json:"resource_id"`
	Zone           string     `gorm:"size:64" json:"zone"`
	Name           string     `gorm:"size:255;index" json:"name"`
	Status         string     `gorm:"size:32;index" json:"status"`
	RawStatus      string     `gorm:"size:64" json:"raw_status"`
	Spec           string     `gorm:"size:128" json:"spec"`
	PrivateIP      string     `gorm:"size:512" json:"private_ip"`
	PublicIP       string     `gorm:"size:512" json:"public_ip"`
	VpcID          string     `gorm:"size:128" json:"vpc_id"`
	ChargeType     string     `gorm:"size:16" json:"charge_type"`
	ExpireAt       *time.Time `gorm:"index" json:"expire_at"`
	CloudCreatedAt *time.Time `json:"cloud_created_at"`
	Tags           StringMap  `gorm:"not null" json:"tags"`
	Extra          JSONObject `gorm:"not null" json:"extra"`
	CPU1h          *float64   `gorm:"column:cpu_1h" json:"cpu_1h"`
	CPU24h         *float64   `gorm:"column:cpu_24h" json:"cpu_24h"`
	MetricsAt      *time.Time `json:"metrics_at"`
	SyncedAt       time.Time  `gorm:"index" json:"synced_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Sync job statuses.
const (
	JobRunning     = "running"
	JobSuccess     = "success"
	JobPartial     = "partial"
	JobFailed      = "failed"
	JobCancelled   = "cancelled"
	JobInterrupted = "interrupted"
)

// Sync job triggers.
const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// SyncJob records one synchronization run of an account.
type SyncJob struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	AccountID   uint       `gorm:"not null;index" json:"account_id"`
	Trigger     string     `gorm:"size:16;not null" json:"trigger"`
	TriggeredBy string     `gorm:"size:64" json:"triggered_by"`
	Status      string     `gorm:"size:16;not null;index" json:"status"`
	TasksTotal  int        `gorm:"not null;default:0" json:"tasks_total"`
	TasksDone   int        `gorm:"not null;default:0" json:"tasks_done"`
	ErrorCount  int        `gorm:"not null;default:0" json:"error_count"`
	Stats       IntMap     `gorm:"not null" json:"stats"`
	Errors      TaskErrors `gorm:"not null" json:"errors"`
	Message     string     `gorm:"type:text" json:"message"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// HostCPUHourly keeps the fleet CPU sum and host count per account and hour,
// so the dashboard can draw an average CPU curve per cloud.
type HostCPUHourly struct {
	ID        uint    `gorm:"primaryKey"`
	AccountID uint    `gorm:"not null;uniqueIndex:uk_cpu_hour,priority:1"`
	Provider  string  `gorm:"size:16;not null;index"`
	Hour      int64   `gorm:"not null;uniqueIndex:uk_cpu_hour,priority:2;index"`
	Sum       float64 `gorm:"not null"`
	Count     int     `gorm:"not null"`
}

// Audit categories.
const (
	AuditLogin   = "login"
	AuditAccount = "account"
	AuditSync    = "sync"
	AuditUser    = "user"
	AuditAlert   = "alert"
)

// AuditLog is an append-only record of a security relevant operation.
type AuditLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"index" json:"user_id"`
	Username  string    `gorm:"size:64;index" json:"username"`
	Category  string    `gorm:"size:16;not null;index" json:"category"`
	Action    string    `gorm:"size:32;not null" json:"action"`
	Result    string    `gorm:"size:16;not null;default:success" json:"result"`
	Target    string    `gorm:"size:255" json:"target"`
	Detail    string    `gorm:"type:text" json:"detail"`
	IP        string    `gorm:"size:64" json:"ip"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// Resource change actions.
const (
	ChangeCreated  = "created"
	ChangeUpdated  = "updated"
	ChangeDeleted  = "deleted"
	ChangeLeftIdle = "left_idle"
)

// ResourceChange records a resource appearing, changing or disappearing
// between two syncs.
type ResourceChange struct {
	ID         uint         `gorm:"primaryKey" json:"id"`
	AccountID  uint         `gorm:"not null;index:idx_change_resource,priority:1" json:"account_id"`
	Provider   string       `gorm:"size:16;not null;index" json:"provider"`
	Type       string       `gorm:"size:16;not null;index:idx_change_resource,priority:2" json:"type"`
	Region     string       `gorm:"size:64;not null" json:"region"`
	ResourceID string       `gorm:"size:255;not null;index:idx_change_resource,priority:3" json:"resource_id"`
	Name       string       `gorm:"size:255" json:"name"`
	Action     string       `gorm:"size:16;not null;index" json:"action"`
	Changes    FieldChanges `gorm:"not null" json:"changes"`
	JobID      uint         `gorm:"index" json:"job_id"`
	CreatedAt  time.Time    `gorm:"index" json:"created_at"`
}

// Notification channel types.
const (
	ChannelFeishu  = "feishu"
	ChannelWebhook = "webhook"
)

// NotifyChannel is where alerts are sent. The webhook URL works as a
// credential, so it is stored encrypted like cloud secrets.
type NotifyChannel struct {
	ID     uint   `gorm:"primaryKey"`
	Name   string `gorm:"size:64;not null"`
	Type   string `gorm:"size:16;not null"`
	URLEnc string `gorm:"size:2048;not null"`
	// SecretEnc is the Feishu signing secret, empty when signing is off.
	SecretEnc string `gorm:"size:1024"`
	Enabled   bool   `gorm:"not null;default:true"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AlertRule is the stored setting of one built-in alert rule.
type AlertRule struct {
	Key        string     `gorm:"primaryKey;size:32"`
	Enabled    bool       `gorm:"not null"`
	Params     JSONObject `gorm:"not null"`
	ChannelIDs UintList   `gorm:"not null"`
	UpdatedAt  time.Time
}

// Alert event statuses.
const (
	AlertFiring   = "firing"
	AlertResolved = "resolved"
)

// AlertEvent is one alert raised by a rule for a resource or an account.
type AlertEvent struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	RuleKey   string `gorm:"size:32;not null;index:idx_alert_open,priority:1" json:"rule"`
	AccountID uint   `gorm:"not null;index:idx_alert_open,priority:2" json:"account_id"`
	Status    string `gorm:"size:16;not null;index:idx_alert_open,priority:3" json:"status"`
	// TargetKey identifies what the alert is about: "type:region:resource_id",
	// or "account" for account level rules.
	TargetKey      string     `gorm:"size:512;not null" json:"target_key"`
	ResourceType   string     `gorm:"size:16" json:"resource_type"`
	Region         string     `gorm:"size:64" json:"region"`
	ResourceID     string     `gorm:"size:255" json:"resource_id"`
	Name           string     `gorm:"size:255" json:"name"`
	Detail         string     `gorm:"type:text" json:"detail"`
	FiredAt        time.Time  `gorm:"index" json:"fired_at"`
	ResolvedAt     *time.Time `json:"resolved_at"`
	NotifyError    string     `gorm:"type:text" json:"notify_error"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
	AcknowledgedBy string     `gorm:"size:64" json:"acknowledged_by"`
	Note           string     `gorm:"type:text" json:"note"`
	SilencedUntil  *time.Time `json:"silenced_until"`
}

// SyncScope retains the last successful collection, including empty inventories.
type SyncScope struct {
	AccountID     uint       `gorm:"primaryKey" json:"account_id"`
	Type          string     `gorm:"primaryKey;size:16" json:"type"`
	Region        string     `gorm:"primaryKey;size:64" json:"region"`
	LastAttemptAt time.Time  `json:"last_attempt_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	Status        string     `gorm:"size:16" json:"status"`
	Error         string     `gorm:"type:text" json:"error"`
}

// AlertDelivery stores the original aggregate message so retries survive restarts.
type AlertDelivery struct {
	ID        uint     `gorm:"primaryKey"`
	AccountID uint     `gorm:"index"`
	ChannelID uint     `gorm:"index"`
	EventIDs  UintList `gorm:"not null"`
	Payload   string   `gorm:"type:text;not null"`
	Status    string   `gorm:"size:16;index"`
	Error     string   `gorm:"type:text"`
	Attempts  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// All lists every model for migrations.
func All() []any {
	return []any{&User{}, &CloudAccount{}, &Resource{}, &SyncJob{}, &HostCPUHourly{}, &AuditLog{}, &ResourceChange{},
		&NotifyChannel{}, &AlertRule{}, &AlertEvent{}, &SyncScope{}, &AlertDelivery{}}
}
