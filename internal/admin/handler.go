// Package admin 提供运维管理 API：渠道运行时 CRUD 与连通性测试（受 admin 令牌保护）。
package admin

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/config"
	"github.com/kingford/TopoLLM/internal/dispatch"
	"github.com/kingford/TopoLLM/internal/security"
)

const testTimeout = 5 * time.Second

// API 是管理接口处理器。
type API struct {
	d     *dispatch.Dispatcher
	guard *security.EgressGuard
	log   *zap.Logger
}

// New 构建管理 API。
func New(d *dispatch.Dispatcher, guard *security.EgressGuard, log *zap.Logger) *API {
	return &API{d: d, guard: guard, log: log}
}

// Register 在给定路由组上挂载管理端点。
func (a *API) Register(r gin.IRouter) {
	r.GET("/channels", a.listChannels)
	r.POST("/channels", a.addChannel)
	r.DELETE("/channels/:name", a.deleteChannel)
	r.POST("/channels/test", a.testChannel)
}

func (a *API) listChannels(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": a.d.List()})
}

func (a *API) addChannel(c *gin.Context) {
	var cc config.ChannelConfig
	if err := c.ShouldBindJSON(&cc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if cc.Name == "" || cc.Adaptor == "" || cc.BaseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name, adaptor, base_url are required"})
		return
	}
	if err := a.guard.Validate(cc.BaseURL); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	cc.Enabled = true
	a.d.Add(cc)
	a.log.Info("channel added via admin API", zap.String("name", cc.Name), zap.String("adaptor", cc.Adaptor))
	c.JSON(http.StatusCreated, gin.H{"status": "added", "name": cc.Name})
}

func (a *API) deleteChannel(c *gin.Context) {
	name := c.Param("name")
	if a.d.Remove(name) {
		a.log.Info("channel removed via admin API", zap.String("name", name))
		c.JSON(http.StatusOK, gin.H{"status": "removed", "name": name})
		return
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "channel not found: " + name})
}

func (a *API) testChannel(c *gin.Context) {
	var cc config.ChannelConfig
	if err := c.ShouldBindJSON(&cc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := a.guard.Validate(cc.BaseURL); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	client := &http.Client{Timeout: testTimeout}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, cc.BaseURL, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"reachable": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	c.JSON(http.StatusOK, gin.H{"reachable": true, "status": resp.StatusCode})
}
