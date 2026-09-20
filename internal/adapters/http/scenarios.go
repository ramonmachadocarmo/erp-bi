package httpadapter

import (
	"net/http"

	"erp/pkg/httpserver"
	"erp/services/bi-service/internal/domain"

	"github.com/gin-gonic/gin"
)

func (h *Handler) registerScenarios(api *gin.RouterGroup) {
	api.GET("/scenarios", h.listScenarios)
	api.POST("/scenarios", h.createScenario)
	api.GET("/scenarios/:id", h.getScenario)
	api.PUT("/scenarios/:id", h.updateScenario)
	api.DELETE("/scenarios/:id", h.deleteScenario)
	api.POST("/scenarios/:id/duplicate", h.duplicateScenario)
	api.PUT("/scenarios/:id/lines/:kind/:target_id", h.setScenarioLine)
	api.DELETE("/scenarios/:id/lines/:kind/:target_id", h.removeScenarioLine)
	api.POST("/scenarios/:id/reset", h.resetScenario)
	api.POST("/scenarios/:id/import-real", h.importRealScenario)
	api.POST("/scenarios/:id/undo", h.undoScenario)
	api.POST("/scenarios/:id/redo", h.redoScenario)
}

func (h *Handler) listScenarios(c *gin.Context) {
	out, err := h.svc.ListScenarios(c.Request.Context())
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) createScenario(c *gin.Context) {
	var in domain.Scenario
	if err := c.ShouldBindJSON(&in); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}
	out, err := h.svc.CreateScenario(c.Request.Context(), in)
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h *Handler) respondScenario(c *gin.Context, out domain.ScenarioResult, err error) {
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) getScenario(c *gin.Context) {
	out, err := h.svc.ScenarioResult(h.withAuth(c), c.Param("id"))
	h.respondScenario(c, out, err)
}

func (h *Handler) updateScenario(c *gin.Context) {
	var in domain.Scenario
	if err := c.ShouldBindJSON(&in); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}
	out, err := h.svc.UpdateScenario(h.withAuth(c), c.Param("id"), in)
	h.respondScenario(c, out, err)
}

func (h *Handler) deleteScenario(c *gin.Context) {
	if err := h.svc.DeleteScenario(c.Request.Context(), c.Param("id")); err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) duplicateScenario(c *gin.Context) {
	var in struct {
		Name string `json:"name"`
	}
	_ = c.ShouldBindJSON(&in)
	out, err := h.svc.DuplicateScenario(c.Request.Context(), c.Param("id"), in.Name)
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h *Handler) setScenarioLine(c *gin.Context) {
	var in struct {
		WeeklyQty *float64 `json:"weekly_qty"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}
	out, err := h.svc.SetScenarioLine(h.withAuth(c), c.Param("id"), c.Param("kind"), c.Param("target_id"), in.WeeklyQty)
	h.respondScenario(c, out, err)
}

func (h *Handler) removeScenarioLine(c *gin.Context) {
	out, err := h.svc.RemoveScenarioLine(h.withAuth(c), c.Param("id"), c.Param("kind"), c.Param("target_id"))
	h.respondScenario(c, out, err)
}

func (h *Handler) resetScenario(c *gin.Context) {
	out, err := h.svc.ResetScenarioToReal(h.withAuth(c), c.Param("id"))
	h.respondScenario(c, out, err)
}

func (h *Handler) importRealScenario(c *gin.Context) {
	out, err := h.svc.ImportRealLines(h.withAuth(c), c.Param("id"))
	h.respondScenario(c, out, err)
}

func (h *Handler) undoScenario(c *gin.Context) {
	out, err := h.svc.UndoScenario(h.withAuth(c), c.Param("id"))
	h.respondScenario(c, out, err)
}

func (h *Handler) redoScenario(c *gin.Context) {
	out, err := h.svc.RedoScenario(h.withAuth(c), c.Param("id"))
	h.respondScenario(c, out, err)
}
