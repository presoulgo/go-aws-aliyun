// Package service contains the business logic behind the HTTP API.
package service

import (
	"strings"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// Actor is the authenticated user performing an operation.
type Actor struct {
	UserID   uint
	Username string
	Role     string
	IP       string
}

// IsAdmin reports whether the actor has the admin role.
func (a Actor) IsAdmin() bool { return a.Role == model.RoleAdmin }

// SystemActor is used for operations started by the server itself.
var SystemActor = Actor{Username: "system", Role: model.RoleAdmin}

// Page normalizes pagination parameters.
type Page struct {
	Page     int
	PageSize int
}

func (p Page) normalize(defSize, maxSize int) Page {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize < 1 {
		p.PageSize = defSize
	}
	if p.PageSize > maxSize {
		p.PageSize = maxSize
	}
	return p
}

func (p Page) offset() int { return (p.Page - 1) * p.PageSize }

// likePattern escapes LIKE wildcards in user input.
func likePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// startOfDay returns local midnight of t.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.In(time.Local).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}
