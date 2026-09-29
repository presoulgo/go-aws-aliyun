package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/service"
)

func (s *Server) metricCatalog(c *gin.Context) {
	defs := s.metrics.Catalog(c.Query("type"))
	list(c, defs, int64(len(defs)))
}

func (s *Server) resourceMetrics(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var keys []string
	for _, k := range strings.Split(c.Query("keys"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	res, err := s.metrics.ForResource(c.Request.Context(), id, keys, c.Query("range"))
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (s *Server) queryMetrics(c *gin.Context) {
	var in service.CompareInput
	if !bindJSON(c, &in) {
		return
	}
	res, err := s.metrics.Compare(c.Request.Context(), in)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (s *Server) dashboardSummary(c *gin.Context) {
	sum, err := s.dashboard.Summary(c.Query("provider"))
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, sum)
}
