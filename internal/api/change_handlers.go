package api

import (
	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/service"
)

func (s *Server) listChanges(c *gin.Context) {
	items, total, err := s.changes.List(service.ChangeFilter{
		Provider:   c.Query("provider"),
		AccountID:  queryUint(c, "account_id"),
		Type:       c.Query("type"),
		Action:     c.Query("action"),
		Region:     c.Query("region"),
		ResourceID: c.Query("resource_id"),
		Range:      c.Query("range"),
		Query:      c.Query("q"),
		Page:       service.Page{Page: queryInt(c, "page", 1), PageSize: queryInt(c, "page_size", 20)},
	})
	if err != nil {
		respondErr(c, err)
		return
	}
	list(c, items, total)
}
