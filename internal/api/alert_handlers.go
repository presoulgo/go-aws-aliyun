package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/service"
)

func (s *Server) listAlertRules(c *gin.Context) {
	rules, err := s.alerts.Rules()
	if err != nil {
		respondErr(c, err)
		return
	}
	list(c, rules, int64(len(rules)))
}

func (s *Server) updateAlertRule(c *gin.Context) {
	var in service.RuleInput
	if !bindJSON(c, &in) {
		return
	}
	if err := s.alerts.UpdateRule(actorOf(c), c.Param("key"), in); err != nil {
		respondErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) listAlertEvents(c *gin.Context) {
	items, total, err := s.alerts.Events(service.EventFilter{
		Status:    c.Query("status"),
		Rule:      c.Query("rule"),
		AccountID: queryUint(c, "account_id"),
		Page:      service.Page{Page: queryInt(c, "page", 1), PageSize: queryInt(c, "page_size", 20)},
	})
	if err != nil {
		respondErr(c, err)
		return
	}
	list(c, items, total)
}

func (s *Server) listChannels(c *gin.Context) {
	items, err := s.alerts.Channels()
	if err != nil {
		respondErr(c, err)
		return
	}
	list(c, items, int64(len(items)))
}

func (s *Server) handleAlertEvent(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var in service.EventHandlingInput
	if !bindJSON(c, &in) {
		return
	}
	if err := s.alerts.HandleEvent(actorOf(c), id, in); err != nil {
		respondErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) retryAlertEvent(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	n, err := s.alerts.RetryEvent(c.Request.Context(), actorOf(c), id)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent": n})
}

func (s *Server) syncHealth(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	items, err := s.syncer.Health(id)
	if err != nil {
		respondErr(c, err)
		return
	}
	list(c, items, int64(len(items)))
}

func (s *Server) createChannel(c *gin.Context) {
	var in service.ChannelInput
	if !bindJSON(c, &in) {
		return
	}
	v, err := s.alerts.CreateChannel(actorOf(c), in)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (s *Server) updateChannel(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var in service.ChannelInput
	if !bindJSON(c, &in) {
		return
	}
	v, err := s.alerts.UpdateChannel(actorOf(c), id, in)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (s *Server) deleteChannel(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := s.alerts.DeleteChannel(actorOf(c), id); err != nil {
		respondErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) testChannel(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := s.alerts.TestChannel(c.Request.Context(), id); err != nil {
		respondErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
