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
}

// Deps are the services the API depends on.
type Deps struct {
	Meta           Meta
	TrustedProxies []string
	Users          *service.UserService
	Audit          *service.AuditService
	Web            fs.FS
}

// Server wires HTTP handlers to services.
type Server struct {
	meta  Meta
	users *service.UserService
	audit *service.AuditService
}

// New builds the HTTP handler.
func New(d Deps) (http.Handler, error) {
	s := &Server{meta: d.Meta, users: d.Users, audit: d.Audit}

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

	r.NoRoute(spaHandler(d.Web))
	return r, nil
}
