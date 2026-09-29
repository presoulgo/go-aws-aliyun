package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/apperr"
	"github.com/presoulgo/go-aws-aliyun/internal/secret"
)

type listResponse[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
}

func list[T any](c *gin.Context, items []T, total int64) {
	if items == nil {
		items = []T{}
	}
	c.JSON(http.StatusOK, listResponse[T]{Items: items, Total: total})
}

func fail(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg})
}

// respondErr renders service errors; unexpected errors are logged and hidden.
func respondErr(c *gin.Context, err error) {
	if e, ok := apperr.As(err); ok {
		if e.Err != nil {
			slog.Warn("请求失败", "path", c.FullPath(), "status", e.Status, "err", e.Err)
		}
		fail(c, e.Status, e.Msg)
		return
	}
	if errors.Is(err, secret.ErrDecrypt) {
		fail(c, http.StatusConflict, err.Error())
		return
	}
	slog.Error("服务器内部错误", "path", c.FullPath(), "err", err)
	fail(c, http.StatusInternalServerError, "服务器内部错误，请稍后重试")
}

func bindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return false
	}
	return true
}

func pathID(c *gin.Context, name string) (uint, bool) {
	n, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || n == 0 {
		fail(c, http.StatusBadRequest, "无效的 ID")
		return 0, false
	}
	return uint(n), true
}

func queryInt(c *gin.Context, name string, def int) int {
	v := c.Query(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func queryUint(c *gin.Context, name string) uint {
	n, err := strconv.ParseUint(c.Query(name), 10, 64)
	if err != nil {
		return 0
	}
	return uint(n)
}
