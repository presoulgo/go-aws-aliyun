package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/service"
)

func (s *Server) listAccounts(c *gin.Context) {
	items, err := s.accounts.List()
	if err != nil {
		respondErr(c, err)
		return
	}
	list(c, items, int64(len(items)))
}

func (s *Server) getAccount(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	v, err := s.accounts.Get(id)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (s *Server) createAccount(c *gin.Context) {
	var in service.AccountInput
	if !bindJSON(c, &in) {
		return
	}
	v, err := s.accounts.Create(c.Request.Context(), actorOf(c), in)
	if err != nil {
		respondErr(c, err)
		return
	}
	if v.Enabled && s.syncer != nil {
		// First sync right away so resources show up without waiting.
		_, _ = s.syncer.Trigger(v.ID, actorOf(c), false)
	}
	c.JSON(http.StatusCreated, v)
}

func (s *Server) updateAccount(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var in service.AccountInput
	if !bindJSON(c, &in) {
		return
	}
	v, err := s.accounts.Update(c.Request.Context(), actorOf(c), id, in)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (s *Server) patchAccount(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if !bindJSON(c, &in) {
		return
	}
	if in.Enabled == nil {
		fail(c, http.StatusBadRequest, "缺少 enabled 字段")
		return
	}
	v, err := s.accounts.SetEnabled(actorOf(c), id, *in.Enabled)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (s *Server) deleteAccount(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if s.syncer != nil {
		s.syncer.CancelAccount(id)
	}
	if err := s.accounts.Delete(actorOf(c), id); err != nil {
		respondErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) testAccount(c *gin.Context) {
	var in service.AccountInput
	if !bindJSON(c, &in) {
		return
	}
	res, err := s.accounts.Test(c.Request.Context(), in)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (s *Server) testExistingAccount(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var in *service.AccountInput
	if c.Request.ContentLength > 0 {
		in = &service.AccountInput{}
		if !bindJSON(c, in) {
			return
		}
	}
	res, err := s.accounts.TestExisting(c.Request.Context(), id, in)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
