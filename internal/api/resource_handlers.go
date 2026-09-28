package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/service"
)

func resourceFilter(c *gin.Context) service.ResourceFilter {
	return service.ResourceFilter{
		Type:      c.Query("type"),
		Provider:  c.Query("provider"),
		AccountID: queryUint(c, "account_id"),
		Region:    c.Query("region"),
		Status:    c.Query("status"),
		Query:     c.Query("q"),
		Idle:      c.Query("idle") == "1" || c.Query("idle") == "true",
		Expiring:  c.Query("expiring") == "1" || c.Query("expiring") == "true",
		Sort:      c.Query("sort"),
		Page:      service.Page{Page: queryInt(c, "page", 1), PageSize: queryInt(c, "page_size", 20)},
	}
}

func (s *Server) listResources(c *gin.Context) {
	items, total, err := s.resources.List(resourceFilter(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	list(c, items, total)
}

func (s *Server) resourceFilters(c *gin.Context) {
	f, err := s.resources.Filters(resourceFilter(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, f)
}

func (s *Server) getResource(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	d, err := s.resources.Get(id)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}
