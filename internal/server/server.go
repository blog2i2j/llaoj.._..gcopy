package server

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/llaoj/gcopy/internal/config"
	"github.com/llaoj/gcopy/internal/gcopy"
	"github.com/sirupsen/logrus"
)

type Server struct {
	cbs *Clipboards
	cfg *config.Config
	log *logrus.Logger
}

func NewServer(log *logrus.Logger) *Server {
	s := &Server{
		cbs: NewClipboards(),
		cfg: config.Get(),
		log: log,
	}

	return s
}

func (s *Server) Run() {
	if s.cfg.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"*"},
		AllowHeaders:     []string{"*"},
		ExposeHeaders:    []string{"*"},
		AllowCredentials: true,
		AllowOriginFunc: func(origin string) bool {
			return true
		},
		MaxAge: 12 * time.Hour,
	}))

	v1 := r.Group("/api/v1")
	v1.GET("/ping", func(c *gin.Context) { c.String(200, "pong") })
	v1.GET("/systeminfo", s.getSystemInfoHandler)

	v1.Use(s.verifyAuth)
	v1.GET("/clipboard", s.getClipboardHandler)
	v1.POST("/clipboard", s.updateClipboardHandler)
	s.log.Info("The server has started!")
	if s.cfg.TLS {
		if err := r.RunTLS(s.cfg.Listen, s.cfg.CertFile, s.cfg.KeyFile); err != nil {
			s.log.Fatal(err)
		}
	} else {
		if err := r.Run(s.cfg.Listen); err != nil {
			s.log.Fatal(err)
		}
	}
}

func (s *Server) getClipboardHandler(c *gin.Context) {
	subject, ok := c.Get("subject")
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"message": "subject not found"})
		return
	}
	sub, ok := subject.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "subject type assert failed"})
		return
	}
	cb := s.cbs.Get(sub)
	if cb == nil {
		c.Header("X-Index", "0")
		c.Status(http.StatusOK)
		return
	}
	index, _ := strconv.Atoi(c.Request.Header.Get("X-Index"))
	if index == cb.Index {
		c.Header("X-Index", strconv.Itoa(cb.Index))
		c.Status(http.StatusOK)
		return
	}

	c.Status(http.StatusOK)
	c.Header("X-Index", strconv.Itoa(cb.Index))
	c.Header("X-Type", cb.Type)
	c.Header("X-FileName", cb.FileName)
	if _, err := c.Writer.Write(cb.Data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "write data failed"})
		s.log.Error(err)
	}
}

func (s *Server) updateClipboardHandler(c *gin.Context) {
	subject, ok := c.Get("subject")
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"message": "subject not found"})
		return
	}
	sub, ok := subject.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "subject type assert failed"})
		return
	}

	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		s.log.Error(err)
	}
	defer c.Request.Body.Close()
	if data == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "request body is nil"})
		return
	}

	xType := c.Request.Header.Get("X-Type")
	xFileName := c.Request.Header.Get("X-FileName")
	if xType == "" || (xType == gcopy.TypeFile && xFileName == "") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "request header invalid"})
		return
	}
	if xType == gcopy.TypeFile && len(data) > 10*1024*1024 {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"message": "the file cannot exceed 10mb"})
		return
	}

	cb := s.cbs.Get(sub)
	index := 0
	if cb != nil {
		index = cb.Index
	}
	cb = &gcopy.Clipboard{
		Index:    index + 1,
		Type:     xType,
		FileName: xFileName,
		Data:     data,
	}
	s.log.Infof("Received %s(%v)", cb.Type, cb.Index)
	s.cbs.Set(sub, cb)

	c.Header("X-Index", strconv.Itoa(cb.Index))
	c.JSON(http.StatusOK, gin.H{"message": "success"})
}
