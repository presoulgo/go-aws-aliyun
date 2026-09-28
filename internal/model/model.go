package model

import "time"

type User struct {
	ID           uint       `json:"id" gorm:"primaryKey"`
	Username     string     `json:"username" gorm:"uniqueIndex;not null"`
	PasswordHash string     `json:"-"`
	Role         string     `json:"role"`
	Enabled      bool       `json:"enabled"`
	FailedLogins int        `json:"failed_logins"`
	LockedUntil  *time.Time `json:"locked_until"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	LastLoginIP  string     `json:"last_login_ip"`
	CreatedAt    time.Time  `json:"created_at"`
}

type CloudAccount struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	Name            string    `json:"name"`
	Provider        string    `json:"provider" gorm:"index"`
	Partition       string    `json:"partition"`
	CredentialType  string    `json:"credential_type"`
	UID             string    `json:"uid"`
	AccessKeyID     string    `json:"access_key_id"`
	SecretEncrypted string    `json:"-"`
	RoleARN         string    `json:"role_arn"`
	Regions         string    `json:"regions"`
	AutoRegions     bool      `json:"auto_regions"`
	Note            string    `json:"note"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Resource struct {
	ID        uint       `json:"id" gorm:"primaryKey"`
	AccountID uint       `json:"account_id" gorm:"uniqueIndex:resource_key"`
	Provider  string     `json:"provider" gorm:"index"`
	Type      string     `json:"type" gorm:"index"`
	Region    string     `json:"region" gorm:"index"`
	CloudID   string     `json:"cloud_id" gorm:"uniqueIndex:resource_key"`
	Name      string     `json:"name" gorm:"index"`
	Status    string     `json:"status" gorm:"index"`
	IP        string     `json:"ip"`
	Spec      string     `json:"spec"`
	VCPU      int        `json:"vcpu"`
	MemoryGB  float64    `json:"memory_gb"`
	Tags      string     `json:"tags"`
	Extra     string     `json:"extra"`
	CPU1H     float64    `json:"cpu_1h"`
	CPU24H    float64    `json:"cpu_24h"`
	MetricsAt *time.Time `json:"metrics_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type SyncJob struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	AccountID     uint       `json:"account_id" gorm:"index"`
	Provider      string     `json:"provider"`
	AccountName   string     `json:"account_name"`
	Status        string     `json:"status"`
	TriggeredBy   string     `json:"triggered_by"`
	TasksTotal    int        `json:"tasks_total"`
	TasksDone     int        `json:"tasks_done"`
	ErrorCount    int        `json:"error_count"`
	ResourceCount int        `json:"resource_count"`
	Errors        string     `json:"errors"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
}

type HostCPUHourly struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	AccountID uint      `json:"account_id" gorm:"uniqueIndex:cpu_key"`
	Provider  string    `json:"provider"`
	Hour      time.Time `json:"hour" gorm:"uniqueIndex:cpu_key"`
	Sum       float64   `json:"sum"`
	Count     int       `json:"count"`
}

type AuditLog struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Category  string    `json:"category" gorm:"index"`
	Action    string    `json:"action"`
	Result    string    `json:"result"`
	Actor     string    `json:"actor"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at" gorm:"index"`
}
