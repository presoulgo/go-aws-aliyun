package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/service"
)

func (s *Server) syncAccount(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	job, err := s.syncer.Trigger(id, actorOf(c), true)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (s *Server) syncAll(c *gin.Context) {
	jobs, err := s.syncer.SyncAll(actorOf(c), true)
	if err != nil {
		respondErr(c, err)
		return
	}
	list(c, jobs, int64(len(jobs)))
}

func (s *Server) cancelJob(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := s.syncer.Cancel(id, actorOf(c)); err != nil {
		respondErr(c, err)
		return
	}
	job, err := s.syncer.Job(id)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, job)
}

func (s *Server) listJobs(c *gin.Context) {
	jobs, total, err := s.syncer.Jobs(queryUint(c, "account_id"), service.Page{Page: queryInt(c, "page", 1), PageSize: queryInt(c, "page_size", 10)})
	if err != nil {
		respondErr(c, err)
		return
	}
	list[model.SyncJob](c, jobs, total)
}

func (s *Server) getJob(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	job, err := s.syncer.Job(id)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, job)
}

func (s *Server) syncStatus(c *gin.Context) {
	st, err := s.syncer.Status()
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, st)
}
