// Package api exposes the REST API under /api/v1 and serves the web UI.
package api

import (
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/service"
)

// Meta is public information shown before login.
type Meta struct {
	Name    string `json:"name"`
	Demo    bool   `json:"demo"`
	Version string `json:"version"`
	// SyncIntervalMinutes and AuditRetentionDays are shown in page hints.
	SyncIntervalMinutes int `json:"sync_interval_minutes"`
	AuditRetentionDays  int `json:"audit_retention_days"`
}

// Deps are the services the API depends on.
type Deps struct {
	Meta           Meta
	TrustedProxies []string
	Users          *service.UserService
	Audit          *service.AuditService
	Accounts       *service.AccountService
	Sync           *service.SyncService
	Resources      *service.ResourceService
	Metrics        *service.MetricsService
	Dashboard      *service.DashboardService
	Web            fs.FS
}

// Server wires HTTP handlers to services.
type Server struct {
	meta      Meta
	users     *service.UserService
	audit     *service.AuditService
	accounts  *service.AccountService
	syncer    *service.SyncService
	resources *service.ResourceService
	metrics   *service.MetricsService
	dashboard *service.DashboardService
}

// New builds the HTTP handler.
func New(d Deps) (http.Handler, error) {
	s := &Server{
		meta: d.Meta, users: d.Users, audit: d.Audit, accounts: d.Accounts, syncer: d.Sync,
		resources: d.Resources, metrics: d.Metrics, dashboard: d.Dashboard,
	}

	r := gin.New()
	if err := r.SetTrustedProxies(d.TrustedProxies); err != nil {
		return nil, err
	}
	r.Use(gin.Recovery(), securityHeaders(), requestLogger())

	v1 := r.Group("/api/v1", limitBody(1<<20))
	v1.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	v1.GET("/meta", func(c *gin.Context) { c.JSON(http.StatusOK, s.meta) })
	v1.POST("/auth/login", s.login)

	authed := v1.Group("", s.authRequired())
	authed.GET("/auth/me", s.me)
	authed.PUT("/auth/password", s.changePassword)

	admin := authed.Group("", adminOnly())
	admin.GET("/users", s.listUsers)
	admin.POST("/users", s.createUser)
	admin.PUT("/users/:id", s.updateUser)
	admin.DELETE("/users/:id", s.deleteUser)
	admin.POST("/users/:id/reset-password", s.resetPassword)
	admin.GET("/audit-logs", s.listAuditLogs)

	if s.accounts != nil {
		authed.GET("/accounts", s.listAccounts)
		authed.GET("/accounts/:id", s.getAccount)
		admin.POST("/accounts", s.createAccount)
		admin.POST("/accounts/test", s.testAccount)
		admin.PUT("/accounts/:id", s.updateAccount)
		admin.PATCH("/accounts/:id", s.patchAccount)
		admin.DELETE("/accounts/:id", s.deleteAccount)
		admin.POST("/accounts/:id/test", s.testExistingAccount)
	}
	if s.syncer != nil {
		admin.POST("/accounts/:id/sync", s.syncAccount)
		admin.POST("/sync/all", s.syncAll)
		admin.POST("/sync-jobs/:id/cancel", s.cancelJob)
		authed.GET("/sync-jobs", s.listJobs)
		authed.GET("/sync-jobs/:id", s.getJob)
		authed.GET("/sync/status", s.syncStatus)
	}
	if s.resources != nil {
		authed.GET("/resources", s.listResources)
		authed.GET("/resources/filters", s.resourceFilters)
		authed.GET("/resources/:id", s.getResource)
	}
	if s.metrics != nil {
		authed.GET("/metrics/catalog", s.metricCatalog)
		authed.GET("/resources/:id/metrics", s.resourceMetrics)
		authed.POST("/metrics/query", s.queryMetrics)
	}
	if s.dashboard != nil {
		authed.GET("/dashboard/summary", s.dashboardSummary)
	}

	r.NoRoute(spaHandler(d.Web))
	return r, nil
}
