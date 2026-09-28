package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	aliyuncloud "github.com/presoulgo/go-aws-aliyun/internal/cloud/aliyun"
	awscloud "github.com/presoulgo/go-aws-aliyun/internal/cloud/aws"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud/demo"
	"github.com/presoulgo/go-aws-aliyun/internal/config"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/secret"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Server struct {
	DB             *gorm.DB
	Config         config.Config
	TokenKey       []byte
	mu             sync.Mutex
	cancels        map[uint]context.CancelFunc
	accountRunning map[uint]uint
	metricCache    map[string]cachedMetric
	providers      map[string]cloud.Provider
}

func New(db *gorm.DB, cfg config.Config) *Server {
	key := cfg.SecretKey
	if key == "" {
		b := make([]byte, 32)
		rand.Read(b)
		key = hex.EncodeToString(b)
	}
	db.Model(&model.SyncJob{}).Where("status = ?", "running").Updates(map[string]any{"status": "cancelled", "errors": "服务重启", "finished_at": time.Now()})
	providers := map[string]cloud.Provider{
		"aws":    awscloud.Provider{},
		"aliyun": aliyuncloud.Provider{},
	}
	if cfg.Demo {
		providers["aws"] = demo.Provider{}
		providers["aliyun"] = demo.Provider{}
	}
	return &Server{DB: db, Config: cfg, TokenKey: []byte(key), cancels: map[uint]context.CancelFunc{}, accountRunning: map[uint]uint{}, metricCache: map[string]cachedMetric{}, providers: providers}
}

func (s *Server) provider(name string) cloud.Provider {
	return s.providers[name]
}

func (s *Server) Router(static http.Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	api := r.Group("/api/v1")
	api.POST("/auth/login", s.login)
	secured := api.Group("")
	secured.Use(s.auth)
	secured.GET("/auth/me", s.me)
	secured.PUT("/auth/password", s.password)
	secured.GET("/dashboard/summary", s.summary)
	secured.GET("/resources", s.resources)
	secured.GET("/resources/filters", s.filters)
	secured.GET("/resources/:id", s.resource)
	secured.GET("/resources/:id/metrics", s.resourceMetrics)
	secured.GET("/metrics/catalog", func(c *gin.Context) { c.JSON(200, gin.H{"items": catalog()}) })
	secured.POST("/metrics/query", s.metricsQuery)
	secured.GET("/accounts", s.accounts)
	secured.GET("/sync-jobs", s.syncJobs)
	secured.GET("/sync/status", s.syncStatus)
	admin := secured.Group("")
	admin.Use(s.admin)
	admin.POST("/accounts/test", s.testAccount)
	admin.POST("/accounts/:id/test", s.testAccount)
	admin.POST("/accounts", s.createAccount)
	admin.PUT("/accounts/:id", s.updateAccount)
	admin.PATCH("/accounts/:id", s.patchAccount)
	admin.DELETE("/accounts/:id", s.deleteAccount)
	admin.POST("/accounts/:id/sync", s.syncAccount)
	admin.POST("/sync/all", s.syncAll)
	admin.POST("/sync-jobs/:id/cancel", s.cancelJob)
	admin.GET("/users", s.users)
	admin.POST("/users", s.createUser)
	admin.PUT("/users/:id", s.updateUser)
	admin.DELETE("/users/:id", s.deleteUser)
	admin.POST("/users/:id/reset-password", s.resetPassword)
	admin.GET("/audit-logs", s.auditLogs)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(404, gin.H{"error": "接口不存在"})
			return
		}
		c.Status(http.StatusOK)
		static.ServeHTTP(c.Writer, c.Request)
	})
	return r
}

func (s *Server) EnsureAdmin() (string, error) {
	var n int64
	s.DB.Model(&model.User{}).Count(&n)
	if n > 0 {
		return "", nil
	}
	p := s.Config.AdminPassword
	if p == "" {
		b := make([]byte, 10)
		rand.Read(b)
		p = hex.EncodeToString(b)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return p, s.DB.Create(&model.User{Username: "admin", PasswordHash: string(h), Role: "admin", Enabled: true}).Error
}

func (s *Server) log(c *gin.Context, category, action, result, target, detail string) {
	actor := "anonymous"
	if v, ok := c.Get("user"); ok {
		actor = v.(model.User).Username
	}
	s.DB.Create(&model.AuditLog{Category: category, Action: action, Result: result, Actor: actor, Target: target, Detail: detail, IP: c.ClientIP()})
}
func fail(c *gin.Context, code int, msg string) { c.JSON(code, gin.H{"error": msg}) }
func id(c *gin.Context) uint                    { n, _ := strconv.ParseUint(c.Param("id"), 10, 64); return uint(n) }
func page(c *gin.Context) (int, int) {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if p < 1 {
		p = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return p, size
}
func user(c *gin.Context) model.User { return c.MustGet("user").(model.User) }

func (s *Server) token(u model.User) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": fmt.Sprint(u.ID), "ver": u.TokenVersion, "exp": time.Now().Add(24 * time.Hour).Unix()}).SignedString(s.TokenKey)
}
func (s *Server) auth(c *gin.Context) {
	raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	t, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != "HS256" {
			return nil, errors.New("invalid method")
		}
		return s.TokenKey, nil
	})
	if err != nil || !t.Valid {
		fail(c, 401, "请先登录")
		c.Abort()
		return
	}
	claims, ok := t.Claims.(jwt.MapClaims)
	if !ok {
		fail(c, 401, "无效凭证")
		c.Abort()
		return
	}
	uid, _ := strconv.Atoi(fmt.Sprint(claims["sub"]))
	var u model.User
	claimVersion, versionOK := claims["ver"].(float64)
	if s.DB.First(&u, uid).Error != nil || !u.Enabled || !versionOK || uint(claimVersion) != u.TokenVersion {
		fail(c, 401, "用户不可用")
		c.Abort()
		return
	}
	c.Set("user", u)
	c.Next()
}
func (s *Server) admin(c *gin.Context) {
	if user(c).Role != "admin" {
		fail(c, 403, "需要管理员权限")
		c.Abort()
		return
	}
	c.Next()
}

func (s *Server) login(c *gin.Context) {
	var in struct{ Username, Password string }
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "请输入用户名和密码")
		return
	}
	var u model.User
	if s.DB.Where("username = ?", in.Username).First(&u).Error != nil {
		fail(c, 401, "用户名或密码错误")
		s.log(c, "login", "login", "failed", in.Username, "unknown user")
		return
	}
	if u.LockedUntil != nil && u.LockedUntil.After(time.Now()) {
		fail(c, 423, "登录失败次数过多，请稍后再试")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		u.FailedLogins++
		if u.FailedLogins >= 5 {
			until := time.Now().Add(15 * time.Minute)
			u.LockedUntil = &until
			u.FailedLogins = 0
		}
		s.DB.Save(&u)
		s.log(c, "login", "login", "failed", u.Username, "bad password")
		fail(c, 401, "用户名或密码错误")
		return
	}
	if !u.Enabled {
		fail(c, 403, "账号已禁用")
		return
	}
	now := time.Now()
	u.LastLoginAt = &now
	u.LastLoginIP = c.ClientIP()
	u.FailedLogins = 0
	u.LockedUntil = nil
	s.DB.Save(&u)
	token, err := s.token(u)
	if err != nil {
		fail(c, 500, "登录失败")
		return
	}
	s.log(c, "login", "login", "success", u.Username, "")
	c.JSON(200, gin.H{"token": token, "user": u, "demo": s.Config.Demo})
}
func (s *Server) me(c *gin.Context) {
	c.JSON(200, gin.H{"user": user(c), "demo": s.Config.Demo, "app_name": s.Config.AppName})
}
func (s *Server) password(c *gin.Context) {
	var in struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if c.ShouldBindJSON(&in) != nil || !validPassword(in.NewPassword) {
		fail(c, 400, "新密码至少 10 位，且必须包含字母和数字")
		return
	}
	u := user(c)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.OldPassword)) != nil {
		fail(c, 400, "旧密码错误")
		return
	}
	h, _ := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
	if err := s.DB.Model(&u).Updates(map[string]any{"password_hash": string(h), "token_version": gorm.Expr("token_version + 1")}).Error; err != nil {
		fail(c, 500, "密码保存失败")
		return
	}
	s.log(c, "user", "change_password", "success", u.Username, "")
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) users(c *gin.Context) {
	var items []model.User
	s.DB.Order("id").Find(&items)
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var failures []struct {
		Target string
		Count  int64
	}
	s.DB.Model(&model.AuditLog{}).Select("target, count(*) as count").Where("category = ? AND action = ? AND result = ? AND created_at >= ?", "login", "login", "failed", start).Group("target").Scan(&failures)
	failureByUser := make(map[string]int64, len(failures))
	for _, failure := range failures {
		failureByUser[failure.Target] = failure.Count
	}
	out := make([]gin.H, 0, len(items))
	for _, item := range items {
		out = append(out, gin.H{
			"id": item.ID, "username": item.Username, "role": item.Role, "enabled": item.Enabled,
			"locked_until": item.LockedUntil, "last_login_at": item.LastLoginAt, "last_login_ip": item.LastLoginIP,
			"today_failed_logins": failureByUser[item.Username], "created_at": item.CreatedAt,
		})
	}
	c.JSON(200, gin.H{"items": out, "total": len(out)})
}
func (s *Server) createUser(c *gin.Context) {
	var in struct{ Username, Password, Role string }
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Username) == "" || !validPassword(in.Password) || !(in.Role == "admin" || in.Role == "viewer") {
		fail(c, 400, "用户名、角色和至少 10 位且包含字母和数字的密码必填")
		return
	}
	h, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	u := model.User{Username: strings.TrimSpace(in.Username), PasswordHash: string(h), Role: in.Role, Enabled: true}
	if err := s.DB.Create(&u).Error; err != nil {
		fail(c, 409, "用户名已存在")
		return
	}
	s.log(c, "user", "create", "success", u.Username, "")
	c.JSON(201, u)
}
func (s *Server) updateUser(c *gin.Context) {
	if id(c) == user(c).ID {
		fail(c, 400, "不能修改自己")
		return
	}
	var in struct {
		Role    string `json:"role"`
		Enabled *bool  `json:"enabled"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "参数错误")
		return
	}
	var u model.User
	if s.DB.First(&u, id(c)).Error != nil {
		fail(c, 404, "用户不存在")
		return
	}
	if in.Role != "" {
		if in.Role != "admin" && in.Role != "viewer" {
			fail(c, 400, "角色无效")
			return
		}
		u.Role = in.Role
	}
	if in.Enabled != nil {
		u.Enabled = *in.Enabled
	}
	s.DB.Save(&u)
	s.log(c, "user", "update", "success", u.Username, "")
	c.JSON(200, u)
}
func (s *Server) deleteUser(c *gin.Context) {
	if id(c) == user(c).ID {
		fail(c, 400, "不能删除自己")
		return
	}
	var u model.User
	if s.DB.First(&u, id(c)).Error != nil {
		fail(c, 404, "用户不存在")
		return
	}
	s.DB.Delete(&u)
	s.log(c, "user", "delete", "success", u.Username, "")
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) resetPassword(c *gin.Context) {
	if id(c) == user(c).ID {
		fail(c, 400, "请使用修改密码")
		return
	}
	var in struct{ Password string }
	if c.ShouldBindJSON(&in) != nil || !validPassword(in.Password) {
		fail(c, 400, "密码至少 10 位，且必须包含字母和数字")
		return
	}
	h, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	result := s.DB.Model(&model.User{}).Where("id = ?", id(c)).Updates(map[string]any{"password_hash": string(h), "token_version": gorm.Expr("token_version + 1"), "failed_logins": 0, "locked_until": nil})
	if result.Error != nil {
		fail(c, 500, "密码保存失败")
		return
	}
	if result.RowsAffected == 0 {
		fail(c, 404, "用户不存在")
		return
	}
	s.log(c, "user", "reset_password", "success", c.Param("id"), "")
	c.JSON(200, gin.H{"ok": true})
}

func validPassword(value string) bool {
	if len([]rune(value)) < 10 {
		return false
	}
	var hasLetter, hasDigit bool
	for _, r := range value {
		hasLetter = hasLetter || unicode.IsLetter(r)
		hasDigit = hasDigit || unicode.IsDigit(r)
	}
	return hasLetter && hasDigit
}
func (s *Server) auditLogs(c *gin.Context) {
	q := s.DB.Model(&model.AuditLog{})
	switch c.Query("range") {
	case "today":
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		q = q.Where("created_at >= ?", start)
	case "7d":
		q = q.Where("created_at >= ?", time.Now().AddDate(0, 0, -7))
	case "30d":
		q = q.Where("created_at >= ?", time.Now().AddDate(0, 0, -30))
	}
	if v := c.Query("category"); v != "" {
		q = q.Where("category = ?", v)
	}
	if v := c.Query("q"); v != "" {
		like := "%" + v + "%"
		q = q.Where("actor LIKE ? OR target LIKE ? OR ip LIKE ?", like, like, like)
	}
	var total int64
	q.Count(&total)
	p, size := page(c)
	var items []model.AuditLog
	q.Order("id desc").Offset((p - 1) * size).Limit(size).Find(&items)
	c.JSON(200, gin.H{"items": items, "total": total})
}

func (s *Server) account(c *gin.Context) (model.CloudAccount, bool) {
	var a model.CloudAccount
	if s.DB.First(&a, id(c)).Error != nil {
		fail(c, 404, "账号不存在")
		return a, false
	}
	return a, true
}
func (s *Server) accounts(c *gin.Context) {
	var items []model.CloudAccount
	s.DB.Order("id").Find(&items)
	out := make([]gin.H, 0, len(items))
	for _, a := range items {
		var count int64
		s.DB.Model(&model.Resource{}).Where("account_id = ?", a.ID).Count(&count)
		var j model.SyncJob
		s.DB.Where("account_id = ?", a.ID).Order("id desc").First(&j)
		item := publicAccount(a)
		item["resource_count"] = count
		item["last_job"] = j
		out = append(out, item)
	}
	c.JSON(200, gin.H{"items": out, "total": len(out)})
}
func publicAccount(a model.CloudAccount) gin.H {
	return gin.H{
		"id": a.ID, "name": a.Name, "provider": a.Provider, "partition": a.Partition,
		"credential_type": a.CredentialType, "uid": a.UID, "access_key_id": mask(a.AccessKeyID),
		"role_arn": a.RoleARN, "regions": a.Regions, "auto_regions": a.AutoRegions,
		"note": a.Note, "enabled": a.Enabled, "created_at": a.CreatedAt, "updated_at": a.UpdatedAt,
	}
}
func mask(v string) string {
	if len(v) <= 4 {
		return "••••"
	}
	return v[:min(4, len(v)-4)] + "••••" + v[len(v)-4:]
}

type accountInput struct {
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Partition      string `json:"partition"`
	CredentialType string `json:"credential_type"`
	AccessKeyID    string `json:"access_key_id"`
	Secret         string `json:"secret"`
	RoleARN        string `json:"role_arn"`
	Regions        string `json:"regions"`
	Note           string `json:"note"`
	AutoRegions    bool   `json:"auto_regions"`
	Enabled        *bool  `json:"enabled"`
}

func (s *Server) testAccount(c *gin.Context) {
	var in accountInput
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "参数错误")
		return
	}
	if in.Provider != "aws" && in.Provider != "aliyun" {
		fail(c, 400, "云厂商无效")
		return
	}
	a := model.CloudAccount{Provider: in.Provider, Partition: in.Partition, AccessKeyID: in.AccessKeyID, RoleARN: in.RoleARN}
	if c.Param("id") != "" {
		existing, ok := s.account(c)
		if !ok {
			return
		}
		if in.AccessKeyID == "" || strings.Contains(in.AccessKeyID, "••••") {
			a.AccessKeyID = existing.AccessKeyID
		}
		if in.Secret == "" {
			plain, err := secret.Decrypt(s.Config.SecretKey, existing.SecretEncrypted)
			if err == nil {
				in.Secret = plain
			}
		}
	}
	if !s.Config.Demo && (a.AccessKeyID == "" || in.Secret == "") {
		fail(c, 400, "AccessKey 必填")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	identity, err := s.provider(in.Provider).Validate(ctx, a, in.Secret)
	if err != nil {
		fail(c, 400, "连接失败: "+err.Error())
		return
	}
	c.JSON(200, identity)
}
func (s *Server) createAccount(c *gin.Context) {
	var in accountInput
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" || (in.Provider != "aws" && in.Provider != "aliyun") {
		fail(c, 400, "账号信息不完整")
		return
	}
	if !s.Config.Demo && in.Secret == "" {
		fail(c, 400, "Secret 必填")
		return
	}
	if !s.Config.Demo && in.AccessKeyID == "" {
		fail(c, 400, "AccessKey ID 必填")
		return
	}
	partition := in.Partition
	if partition == "" {
		partition = "global"
	}
	if partition != "global" && partition != "china" {
		fail(c, 400, "分区无效")
		return
	}
	enc := ""
	if in.Secret != "" {
		var err error
		enc, err = secret.Encrypt(s.Config.SecretKey, in.Secret)
		if err != nil {
			fail(c, 500, err.Error())
			return
		}
	}
	credentialType := "access_key"
	if strings.TrimSpace(in.RoleARN) != "" {
		credentialType = "role"
	}
	a := model.CloudAccount{Name: strings.TrimSpace(in.Name), Provider: in.Provider, Partition: partition, CredentialType: credentialType, AccessKeyID: strings.TrimSpace(in.AccessKeyID), SecretEncrypted: enc, RoleARN: strings.TrimSpace(in.RoleARN), Regions: strings.Join(splitRegions(in.Regions), ","), AutoRegions: in.AutoRegions, Note: strings.TrimSpace(in.Note), Enabled: true}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	identity, err := s.provider(in.Provider).Validate(ctx, a, in.Secret)
	if err != nil {
		fail(c, 400, "连接失败: "+err.Error())
		return
	}
	a.UID = identity.UID
	if a.Regions == "" {
		if !a.AutoRegions {
			fail(c, 400, "请至少选择一个同步地域")
			return
		}
		a.Regions = strings.Join(identity.Regions, ",")
	}
	if err := s.DB.Create(&a).Error; err != nil {
		fail(c, 500, "保存失败")
		return
	}
	s.log(c, "account", "create", "success", a.Name, "")
	job := s.startSync(a, user(c).Username)
	s.log(c, "sync", "start", "success", a.Name, fmt.Sprint(job.ID))
	result := publicAccount(a)
	result["sync_job"] = job
	c.JSON(201, result)
}
func (s *Server) updateAccount(c *gin.Context) {
	a, ok := s.account(c)
	if !ok {
		return
	}
	var in accountInput
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "参数错误")
		return
	}
	before := a
	if strings.TrimSpace(in.Name) != "" {
		a.Name = strings.TrimSpace(in.Name)
	}
	if in.Partition != "" {
		if in.Partition != "global" && in.Partition != "china" {
			fail(c, 400, "分区无效")
			return
		}
		a.Partition = in.Partition
	}
	if in.AccessKeyID != "" && !strings.Contains(in.AccessKeyID, "••••") {
		a.AccessKeyID = strings.TrimSpace(in.AccessKeyID)
	}
	a.RoleARN = strings.TrimSpace(in.RoleARN)
	if strings.TrimSpace(a.RoleARN) != "" {
		a.CredentialType = "role"
	} else {
		a.CredentialType = "access_key"
	}
	if in.Regions != "" {
		a.Regions = strings.Join(splitRegions(in.Regions), ",")
	}
	a.AutoRegions = in.AutoRegions
	a.Note = in.Note
	if in.Secret != "" {
		enc, err := secret.Encrypt(s.Config.SecretKey, in.Secret)
		if err != nil {
			fail(c, 500, err.Error())
			return
		}
		a.SecretEncrypted = enc
	}
	if a.AccessKeyID != before.AccessKeyID || a.SecretEncrypted != before.SecretEncrypted || a.RoleARN != before.RoleARN || a.Partition != before.Partition {
		plain := in.Secret
		if plain == "" {
			plain, _ = secret.Decrypt(s.Config.SecretKey, a.SecretEncrypted)
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		identity, err := s.provider(a.Provider).Validate(ctx, a, plain)
		if err != nil {
			fail(c, 400, "连接失败: "+err.Error())
			return
		}
		a.UID = identity.UID
	}
	if err := s.DB.Save(&a).Error; err != nil {
		fail(c, 500, "保存失败")
		return
	}
	changes := []string{}
	add := func(label, old, new string) {
		if old != new {
			changes = append(changes, label+": "+old+" → "+new)
		}
	}
	add("名称", before.Name, a.Name)
	add("分区", before.Partition, a.Partition)
	add("AccessKey", mask(before.AccessKeyID), mask(a.AccessKeyID))
	add("角色 ARN", before.RoleARN, a.RoleARN)
	add("地域", before.Regions, a.Regions)
	add("自动地域", fmt.Sprint(before.AutoRegions), fmt.Sprint(a.AutoRegions))
	add("备注", before.Note, a.Note)
	if in.Secret != "" {
		changes = append(changes, "Secret 已更换")
	}
	s.log(c, "account", "update", "success", a.Name, strings.Join(changes, "; "))
	c.JSON(200, publicAccount(a))
}
func (s *Server) patchAccount(c *gin.Context) {
	a, ok := s.account(c)
	if !ok {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "参数错误")
		return
	}
	a.Enabled = in.Enabled
	if err := s.DB.Save(&a).Error; err != nil {
		fail(c, 500, "保存失败")
		return
	}
	if !in.Enabled {
		s.mu.Lock()
		jobID := s.accountRunning[a.ID]
		cancel := s.cancels[jobID]
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	s.log(c, "account", "toggle", "success", a.Name, fmt.Sprint(in.Enabled))
	c.JSON(200, publicAccount(a))
}
func (s *Server) deleteAccount(c *gin.Context) {
	a, ok := s.account(c)
	if !ok {
		return
	}
	s.mu.Lock()
	runningJob := s.accountRunning[a.ID]
	s.mu.Unlock()
	if runningJob != 0 {
		fail(c, http.StatusConflict, "账号正在同步，请先取消同步任务")
		return
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("account_id = ?", a.ID).Delete(&model.Resource{}).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", a.ID).Delete(&model.HostCPUHourly{}).Error; err != nil {
			return err
		}
		return tx.Delete(&a).Error
	}); err != nil {
		fail(c, 500, "删除失败")
		return
	}
	s.log(c, "account", "delete", "success", a.Name, "")
	c.JSON(200, gin.H{"ok": true})
}
