package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/service"
)

func (s *Server) login(c *gin.Context) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !bindJSON(c, &in) {
		return
	}
	res, err := s.users.Login(in.Username, in.Password, c.ClientIP())
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (s *Server) me(c *gin.Context) {
	c.JSON(http.StatusOK, currentUser(c))
}

func (s *Server) changePassword(c *gin.Context) {
	var in struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !bindJSON(c, &in) {
		return
	}
	res, err := s.users.ChangePassword(actorOf(c), in.OldPassword, in.NewPassword)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (s *Server) listUsers(c *gin.Context) {
	users, err := s.users.List(actorOf(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	list(c, users, int64(len(users)))
}

func (s *Server) createUser(c *gin.Context) {
	var in service.CreateUserInput
	if !bindJSON(c, &in) {
		return
	}
	u, err := s.users.Create(actorOf(c), in)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, u)
}

func (s *Server) updateUser(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var in service.UpdateUserInput
	if !bindJSON(c, &in) {
		return
	}
	u, err := s.users.Update(actorOf(c), id, in)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, u)
}

func (s *Server) deleteUser(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := s.users.Delete(actorOf(c), id); err != nil {
		respondErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) resetPassword(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if c.Request.ContentLength > 0 && !bindJSON(c, &in) {
		return
	}
	pw, err := s.users.ResetPassword(actorOf(c), id, in.Password)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"password": pw})
}

func (s *Server) listAuditLogs(c *gin.Context) {
	items, total, err := s.audit.List(service.AuditFilter{
		Range:    c.Query("range"),
		Category: c.Query("category"),
		Query:    c.Query("q"),
		Page:     service.Page{Page: queryInt(c, "page", 1), PageSize: queryInt(c, "page_size", 20)},
	})
	if err != nil {
		respondErr(c, err)
		return
	}
	list(c, items, total)
}
