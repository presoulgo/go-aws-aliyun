package service

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/presoulgo/go-aws-aliyun/internal/apperr"
	"github.com/presoulgo/go-aws-aliyun/internal/auth"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,31}$`)

// UserService manages users, logins and passwords.
type UserService struct {
	db      *gorm.DB
	audit   *AuditService
	tokens  *auth.Tokens
	limiter *auth.Limiter
	lockFor time.Duration
	now     func() time.Time
}

func NewUserService(db *gorm.DB, audit *AuditService, tokens *auth.Tokens, limiter *auth.Limiter, lockFor time.Duration) *UserService {
	return &UserService{db: db, audit: audit, tokens: tokens, limiter: limiter, lockFor: lockFor, now: time.Now}
}

// EnsureAdmin creates the initial admin when the user table is empty. It
// returns the generated password when none was configured.
func (s *UserService) EnsureAdmin(configured string) (created bool, generated string, err error) {
	var count int64
	if err := s.db.Model(&model.User{}).Count(&count).Error; err != nil {
		return false, "", err
	}
	if count > 0 {
		return false, "", nil
	}
	password := configured
	if password == "" {
		if password, err = auth.GeneratePassword(14); err != nil {
			return false, "", err
		}
		generated = password
	} else if err := auth.ValidatePassword(password); err != nil {
		return false, "", fmt.Errorf("初始管理员密码不符合要求: %w", err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return false, "", err
	}
	u := model.User{Username: "admin", DisplayName: "系统管理员", PasswordHash: hash, Role: model.RoleAdmin}
	if err := s.db.Create(&u).Error; err != nil {
		return false, "", err
	}
	return true, generated, nil
}

// LoginResult is returned after a successful login.
type LoginResult struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      *model.User `json:"user"`
}

// Login verifies credentials, enforcing the lockout policy.
func (s *UserService) Login(username, password, ip string) (*LoginResult, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	actor := Actor{Username: username, IP: ip}
	if username == "" || password == "" {
		return nil, apperr.Invalid("请输入用户名和密码")
	}
	if until, locked := s.limiter.LockedUntil(username); locked {
		s.audit.Record(actor, AuditEntry{Category: model.AuditLogin, Action: ActLoginFailed, Failed: true, Detail: "账号锁定中"})
		return nil, apperr.TooMany(fmt.Sprintf("登录失败次数过多，请 %d 分钟后再试", minutesUntil(s.now(), until)))
	}

	var u model.User
	err := s.db.Where("username = ?", username).First(&u).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	found := err == nil
	if !found || !auth.CheckPassword(u.PasswordHash, password) {
		remaining, until := s.limiter.Fail(username)
		detail := "密码错误"
		if !found {
			detail = "用户不存在"
		}
		maxFailures := s.limiter.MaxFailures()
		if !until.IsZero() {
			detail += fmt.Sprintf("（连续第 %d 次，账号锁定 %d 分钟）", maxFailures, minutesUntil(s.now(), until))
		} else {
			detail += fmt.Sprintf("（连续第 %d 次，%d 次后锁定 %d 分钟）", maxFailures-remaining, maxFailures, int(s.lockFor.Minutes()))
		}
		if found {
			actor.UserID = u.ID
		}
		s.audit.Record(actor, AuditEntry{Category: model.AuditLogin, Action: ActLoginFailed, Failed: true, Detail: detail})
		switch {
		case !until.IsZero():
			return nil, apperr.TooMany(fmt.Sprintf("密码错误次数过多，账号已锁定 %d 分钟", minutesUntil(s.now(), until)))
		case remaining <= 2:
			return nil, apperr.Unauthorized(fmt.Sprintf("用户名或密码错误，再错 %d 次将锁定账号", remaining))
		default:
			return nil, apperr.Unauthorized("用户名或密码错误")
		}
	}
	actor.UserID = u.ID
	if u.Disabled {
		s.audit.Record(actor, AuditEntry{Category: model.AuditLogin, Action: ActLoginFailed, Failed: true, Detail: "账号已禁用"})
		return nil, apperr.Forbidden("账号已被禁用，请联系管理员")
	}
	s.limiter.Reset(username)
	now := s.now().UTC()
	if err := s.db.Model(&u).Updates(map[string]any{"last_login_at": now, "last_login_ip": ip}).Error; err != nil {
		return nil, err
	}
	u.LastLoginAt, u.LastLoginIP = &now, ip
	token, exp, err := s.tokens.Issue(u.ID, u.Username, u.Role, u.TokenVersion)
	if err != nil {
		return nil, err
	}
	s.audit.Record(actor, AuditEntry{Category: model.AuditLogin, Action: ActLoginSuccess})
	return &LoginResult{Token: token, ExpiresAt: exp, User: &u}, nil
}

// Authenticate resolves a bearer token to an active user.
func (s *UserService) Authenticate(token string) (*model.User, error) {
	claims, err := s.tokens.Parse(token)
	if err != nil {
		return nil, apperr.Unauthorized(auth.ErrInvalidToken.Error())
	}
	var u model.User
	if err := s.db.First(&u, claims.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.Unauthorized(auth.ErrInvalidToken.Error())
		}
		return nil, err
	}
	if u.Disabled || u.TokenVersion != claims.Version {
		return nil, apperr.Unauthorized(auth.ErrInvalidToken.Error())
	}
	return &u, nil
}

// UserView adds derived fields to a user for listing.
type UserView struct {
	model.User
	FailedToday int  `json:"failed_today"`
	Me          bool `json:"me"`
}

// List returns all users with today's failed login counts.
func (s *UserService) List(actor Actor) ([]UserView, error) {
	var users []model.User
	if err := s.db.Order("id ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	failed, err := s.audit.FailedLoginsToday()
	if err != nil {
		return nil, err
	}
	out := make([]UserView, 0, len(users))
	for _, u := range users {
		out = append(out, UserView{User: u, FailedToday: failed[u.Username], Me: u.ID == actor.UserID})
	}
	return out, nil
}

// CreateUserInput holds the fields for a new user.
type CreateUserInput struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Role        string `json:"role"`
}

// Create adds a user.
func (s *UserService) Create(actor Actor, in CreateUserInput) (*model.User, error) {
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if !usernameRe.MatchString(username) {
		return nil, apperr.Invalid("用户名需为 2~32 位小写字母、数字、点、下划线或短横线，且以字母或数字开头")
	}
	if !validRole(in.Role) {
		return nil, apperr.Invalid("角色无效")
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		return nil, apperr.Invalid(err.Error())
	}
	var exists int64
	if err := s.db.Model(&model.User{}).Where("username = ?", username).Count(&exists).Error; err != nil {
		return nil, err
	}
	if exists > 0 {
		return nil, apperr.Conflict("用户名已存在")
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	u := model.User{
		Username:     username,
		DisplayName:  truncate(strings.TrimSpace(in.DisplayName), 128),
		PasswordHash: hash,
		Role:         in.Role,
	}
	if err := s.db.Create(&u).Error; err != nil {
		return nil, err
	}
	s.audit.Record(actor, AuditEntry{Category: model.AuditUser, Action: ActUserCreate, Target: u.Username, Detail: "角色：" + roleLabel(u.Role)})
	return &u, nil
}

// UpdateUserInput holds optional changes to a user.
type UpdateUserInput struct {
	DisplayName *string `json:"display_name"`
	Role        *string `json:"role"`
	Disabled    *bool   `json:"disabled"`
}

// Update changes a user's display name, role or disabled flag.
func (s *UserService) Update(actor Actor, id uint, in UpdateUserInput) (*model.User, error) {
	var out *model.User
	// The audit entry is written after the commit: the audit service uses its
	// own connection and SQLite would block it on this transaction's lock.
	var entry *AuditEntry
	err := s.db.Transaction(func(tx *gorm.DB) error {
		u, err := findUser(tx, id)
		if err != nil {
			return err
		}
		updates := map[string]any{}
		var changes []string
		bump := false
		if in.DisplayName != nil {
			name := truncate(strings.TrimSpace(*in.DisplayName), 128)
			if name != u.DisplayName {
				changes = append(changes, fmt.Sprintf("显示名称：%s → %s", orDash(u.DisplayName), orDash(name)))
				updates["display_name"] = name
			}
		}
		if in.Role != nil && *in.Role != u.Role {
			if !validRole(*in.Role) {
				return apperr.Invalid("角色无效")
			}
			if u.ID == actor.UserID {
				return apperr.Forbidden("不能修改自己的角色")
			}
			if u.Role == model.RoleAdmin {
				if err := ensureAnotherAdmin(tx, u.ID); err != nil {
					return err
				}
			}
			changes = append(changes, fmt.Sprintf("角色：%s → %s", roleLabel(u.Role), roleLabel(*in.Role)))
			updates["role"] = *in.Role
			bump = true
		}
		action := ActUserUpdate
		if in.Disabled != nil && *in.Disabled != u.Disabled {
			if u.ID == actor.UserID {
				return apperr.Forbidden("不能禁用自己")
			}
			if *in.Disabled && u.Role == model.RoleAdmin {
				if err := ensureAnotherAdmin(tx, u.ID); err != nil {
					return err
				}
			}
			updates["disabled"] = *in.Disabled
			bump = true
			if *in.Disabled {
				action = ActUserDisable
				changes = append(changes, "状态：正常 → 已禁用")
			} else {
				action = ActUserEnable
				changes = append(changes, "状态：已禁用 → 正常")
			}
		}
		if len(updates) == 0 {
			out = u
			return nil
		}
		if bump {
			updates["token_version"] = gorm.Expr("token_version + 1")
		}
		if err := tx.Model(u).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.First(u, u.ID).Error; err != nil {
			return err
		}
		out = u
		entry = &AuditEntry{Category: model.AuditUser, Action: action, Target: u.Username, Detail: strings.Join(changes, "；")}
		return nil
	})
	if err == nil && entry != nil {
		s.audit.Record(actor, *entry)
	}
	return out, err
}

// Delete removes a user.
func (s *UserService) Delete(actor Actor, id uint) error {
	var username string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		u, err := findUser(tx, id)
		if err != nil {
			return err
		}
		username = u.Username
		if u.ID == actor.UserID {
			return apperr.Forbidden("不能删除自己")
		}
		if u.Role == model.RoleAdmin {
			if err := ensureAnotherAdmin(tx, u.ID); err != nil {
				return err
			}
		}
		return tx.Delete(u).Error
	})
	if err != nil {
		return err
	}
	s.audit.Record(actor, AuditEntry{Category: model.AuditUser, Action: ActUserDelete, Target: username})
	return nil
}

// ResetPassword sets a new password (generated when empty) and returns it.
func (s *UserService) ResetPassword(actor Actor, id uint, password string) (string, error) {
	u, err := findUser(s.db, id)
	if err != nil {
		return "", err
	}
	if password == "" {
		if password, err = auth.GeneratePassword(12); err != nil {
			return "", err
		}
	} else if err := auth.ValidatePassword(password); err != nil {
		return "", apperr.Invalid(err.Error())
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}
	err = s.db.Model(u).Updates(map[string]any{"password_hash": hash, "token_version": gorm.Expr("token_version + 1")}).Error
	if err != nil {
		return "", err
	}
	s.limiter.Reset(u.Username)
	s.audit.Record(actor, AuditEntry{Category: model.AuditUser, Action: ActUserResetPassword, Target: u.Username})
	return password, nil
}

// ChangePassword lets a user change their own password and returns a fresh
// login token (older tokens stop working).
func (s *UserService) ChangePassword(actor Actor, oldPassword, newPassword string) (*LoginResult, error) {
	u, err := findUser(s.db, actor.UserID)
	if err != nil {
		return nil, err
	}
	if !auth.CheckPassword(u.PasswordHash, oldPassword) {
		return nil, apperr.Invalid("当前密码不正确")
	}
	if err := auth.ValidatePassword(newPassword); err != nil {
		return nil, apperr.Invalid(err.Error())
	}
	if oldPassword == newPassword {
		return nil, apperr.Invalid("新密码不能与当前密码相同")
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	err = s.db.Model(u).Updates(map[string]any{"password_hash": hash, "token_version": gorm.Expr("token_version + 1")}).Error
	if err != nil {
		return nil, err
	}
	if err := s.db.First(u, u.ID).Error; err != nil {
		return nil, err
	}
	token, exp, err := s.tokens.Issue(u.ID, u.Username, u.Role, u.TokenVersion)
	if err != nil {
		return nil, err
	}
	s.audit.Record(actor, AuditEntry{Category: model.AuditUser, Action: ActPasswordChange, Target: u.Username, Detail: "本人修改"})
	return &LoginResult{Token: token, ExpiresAt: exp, User: u}, nil
}

func findUser(db *gorm.DB, id uint) (*model.User, error) {
	var u model.User
	if err := db.First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("用户不存在")
		}
		return nil, err
	}
	return &u, nil
}

// ensureAnotherAdmin fails if removing admin rights from exceptID would leave
// no enabled administrator.
func ensureAnotherAdmin(tx *gorm.DB, exceptID uint) error {
	var n int64
	err := tx.Model(&model.User{}).Where("role = ? AND disabled = ? AND id <> ?", model.RoleAdmin, false, exceptID).Count(&n).Error
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.Conflict("至少需要保留一个启用的管理员")
	}
	return nil
}

func validRole(r string) bool { return r == model.RoleAdmin || r == model.RoleViewer }

func roleLabel(r string) string {
	if r == model.RoleAdmin {
		return "管理员"
	}
	return "只读"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func minutesUntil(now, t time.Time) int {
	m := int(math.Ceil(t.Sub(now).Minutes()))
	if m < 1 {
		m = 1
	}
	return m
}
