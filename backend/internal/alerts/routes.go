package alerts

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"step-ca-webui/internal/api"
	"step-ca-webui/internal/db"

	"github.com/gin-gonic/gin"
)

// Routes returns this feature's API routes, for api.SetupRoutes. Reading is
// open to any logged-in account (viewers get a read-only summary with no
// secrets); changing settings, testing and "check now" are admin-only.
func (s *Service) Routes() api.RouteExtension {
	return func(loggedIn, admin *gin.RouterGroup) {
		loggedIn.GET("/alerts/schema", s.handleSchema)
		loggedIn.GET("/alerts/config", s.handleGetConfig)
		loggedIn.GET("/alerts/status", s.handleStatus)

		admin.PUT("/alerts/config", s.handlePutConfig)
		admin.POST("/alerts/check", s.handleCheck)
		admin.POST("/alerts/destinations/:id/test", s.handleTest)
	}
}

func isAdmin(c *gin.Context) bool {
	u := api.CurrentUser(c)
	return u != nil && u.Role == db.RoleAdmin
}

func actor(c *gin.Context) string {
	if u := api.CurrentUser(c); u != nil {
		return u.Username
	}
	return "system"
}

func (s *Service) handleSchema(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"types": schemas})
}

func (s *Service) handleGetConfig(c *gin.Context) {
	cfg, err := s.loadConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load alert settings"})
		return
	}
	c.JSON(http.StatusOK, toView(cfg, s.store))
}

func (s *Service) handleStatus(c *gin.Context) {
	st, err := s.Status(isAdmin(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load alert status"})
		return
	}
	c.JSON(http.StatusOK, st)
}

func (s *Service) handlePutConfig(c *gin.Context) {
	var up ConfigUpdate
	if err := c.ShouldBindJSON(&up); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	cur, err := s.loadConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load alert settings"})
		return
	}
	next, err := applyUpdate(cur, up, s.store)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.store.Put(settingsKey, next, actor(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save alert settings"})
		return
	}

	var names []string
	for _, d := range next.Destinations {
		state := "off"
		if d.Enabled {
			state = "on"
		}
		names = append(names, fmt.Sprintf("%s (%s, %s)", d.Name, d.Type, state))
	}
	onOff := "off"
	if next.Enabled {
		onOff = "on"
	}
	_ = s.db.Create(&db.AuditEvent{Who: actor(c), Action: "alerts_settings_changed", Timestamp: time.Now(),
		Details: fmt.Sprintf("alerts %s; thresholds %v; destinations: %s", onOff, next.Thresholds, strings.Join(names, ", "))}).Error

	c.JSON(http.StatusOK, toView(next, s.store))
}

func (s *Service) handleCheck(c *gin.Context) {
	res, err := s.Check(c.Request.Context(), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "check failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (s *Service) handleTest(c *gin.Context) {
	if err := s.Test(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "test alert sent"})
}
