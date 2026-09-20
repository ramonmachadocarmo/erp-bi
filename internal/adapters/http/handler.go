package httpadapter

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"erp/pkg/httpserver"
	purchasingclient "erp/services/bi-service/internal/adapters/purchasing"
	salesclient "erp/services/bi-service/internal/adapters/sales"
	stockclient "erp/services/bi-service/internal/adapters/stock"
	"erp/services/bi-service/internal/application"
	"erp/services/bi-service/internal/domain"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *application.Service
}

func New(svc *application.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(r *gin.Engine, jwt gin.HandlerFunc) {
	api := r.Group("/", jwt)
	api.GET("/forecasts", h.listForecasts)
	api.PUT("/forecasts/:kind/:target_id", h.setForecastOverride)
	api.DELETE("/forecasts/:kind/:target_id", h.clearForecastOverride)
	api.POST("/forecasts/:kind/:target_id/exclude", h.excludeForecast)
	api.DELETE("/forecasts/:kind/:target_id/exclude", h.includeForecast)
	api.GET("/storage-plan", h.storagePlan)
	api.GET("/financials", h.financials)
	api.GET("/supplier-prices", h.listSupplierPrices)
	api.POST("/supplier-prices", h.setSupplierPrice)
	api.DELETE("/supplier-prices/:id", h.deleteSupplierPrice)
	api.GET("/budgets", h.listBudgets)
	api.POST("/budgets", h.createBudget)
	api.GET("/budgets/:id", h.getBudget)
	api.DELETE("/budgets/:id", h.deleteBudget)
	api.PUT("/budgets/:id/items/:item_id", h.updateBudgetItem)
	api.DELETE("/budgets/:id/items/:item_id", h.deleteBudgetItem)
	api.POST("/budgets/:id/items/:item_id/allocations", h.addAllocation)
	api.DELETE("/budgets/:id/allocations/:allocation_id", h.deleteAllocation)
	api.POST("/budgets/:id/confirm", h.confirmBudget)
	api.POST("/budgets/:id/cancel", h.cancelBudget)
	api.GET("/schedules", h.listSchedules)
	api.POST("/schedules", h.createSchedule)
	api.GET("/schedules/:id", h.getSchedule)
	api.PUT("/schedules/:id", h.updateSchedule)
	api.DELETE("/schedules/:id", h.deleteSchedule)
	api.POST("/schedules/:id/run-now", h.runScheduleNow)
	h.registerScenarios(api)
}

func (h *Handler) withAuth(c *gin.Context) context.Context {
	ctx := context.WithValue(c.Request.Context(), salesclient.AuthHeaderKey, c.GetHeader("Authorization"))
	ctx = context.WithValue(ctx, stockclient.AuthHeaderKey, c.GetHeader("Authorization"))
	return context.WithValue(ctx, purchasingclient.AuthHeaderKey, c.GetHeader("Authorization"))
}

func status(err error) int {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrInUse):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func intQuery(c *gin.Context, key string, def int) int {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func floatQuery(c *gin.Context, key string, def float64) float64 {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return n
}

func (h *Handler) listForecasts(c *gin.Context) {
	lookback := intQuery(c, "lookback_weeks", 8)
	includeExcluded := c.Query("include_excluded") == "true"
	out, err := h.svc.ListForecasts(h.withAuth(c), lookback, includeExcluded)
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) excludeForecast(c *gin.Context) {
	if err := h.svc.ExcludeFromForecast(c.Request.Context(), c.Param("kind"), c.Param("target_id")); err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) includeForecast(c *gin.Context) {
	if err := h.svc.IncludeInForecast(c.Request.Context(), c.Param("kind"), c.Param("target_id")); err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) setForecastOverride(c *gin.Context) {
	var in struct {
		WeeklyQty float64 `json:"weekly_qty"`
		Note      string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}
	updatedBy := c.GetString("email")
	out, err := h.svc.SetForecastOverride(c.Request.Context(), c.Param("kind"), c.Param("target_id"), in.WeeklyQty, in.Note, updatedBy)
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) clearForecastOverride(c *gin.Context) {
	if err := h.svc.ClearForecastOverride(c.Request.Context(), c.Param("kind"), c.Param("target_id")); err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) listSupplierPrices(c *gin.Context) {
	out, err := h.svc.ListSupplierPrices(c.Request.Context())
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) setSupplierPrice(c *gin.Context) {
	var in struct {
		ProductID  string  `json:"product_id"`
		SupplierID string  `json:"supplier_id"`
		Price      float64 `json:"price"`
		MinQty     float64 `json:"min_qty"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}
	out, err := h.svc.SetSupplierPrice(c.Request.Context(), in.ProductID, in.SupplierID, in.Price, in.MinQty)
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) deleteSupplierPrice(c *gin.Context) {
	if err := h.svc.DeleteSupplierPrice(c.Request.Context(), c.Param("id")); err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) storagePlan(c *gin.Context) {
	coverage := intQuery(c, "coverage_weeks", 4)
	safety := floatQuery(c, "safety_percent", 0)
	lookback := intQuery(c, "lookback_weeks", 8)
	out, err := h.svc.StoragePlan(h.withAuth(c), coverage, safety, lookback)
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) financials(c *gin.Context) {
	lookback := intQuery(c, "lookback_weeks", 8)
	out, err := h.svc.Financials(h.withAuth(c), lookback)
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) listBudgets(c *gin.Context) {
	out, err := h.svc.ListBudgets(c.Request.Context())
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) createBudget(c *gin.Context) {
	var in struct {
		CoverageWeeks int     `json:"coverage_weeks"`
		SafetyPercent float64 `json:"safety_percent"`
		LookbackWeeks int     `json:"lookback_weeks"`
	}
	_ = c.ShouldBindJSON(&in)
	if in.CoverageWeeks == 0 {
		in.CoverageWeeks = 4
	}
	if in.LookbackWeeks == 0 {
		in.LookbackWeeks = 8
	}
	out, err := h.svc.GenerateBudget(h.withAuth(c), in.CoverageWeeks, in.SafetyPercent, in.LookbackWeeks, nil)
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h *Handler) getBudget(c *gin.Context) {
	out, err := h.svc.GetBudget(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) updateBudgetItem(c *gin.Context) {
	var in struct {
		NeededQty float64 `json:"needed_qty"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}
	out, err := h.svc.UpdateBudgetItem(c.Request.Context(), c.Param("id"), c.Param("item_id"), in.NeededQty)
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) deleteBudgetItem(c *gin.Context) {
	if err := h.svc.DeleteBudgetItem(c.Request.Context(), c.Param("id"), c.Param("item_id")); err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) addAllocation(c *gin.Context) {
	var in struct {
		SupplierID string  `json:"supplier_id"`
		Quantity   float64 `json:"quantity"`
		UnitPrice  float64 `json:"unit_price"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}
	out, err := h.svc.AllocateBudgetItem(c.Request.Context(), c.Param("id"), c.Param("item_id"), in.SupplierID, in.Quantity, in.UnitPrice)
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h *Handler) deleteAllocation(c *gin.Context) {
	if err := h.svc.DeleteBudgetAllocation(c.Request.Context(), c.Param("id"), c.Param("allocation_id")); err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) confirmBudget(c *gin.Context) {
	out, err := h.svc.ConfirmBudget(h.withAuth(c), c.Param("id"))
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) cancelBudget(c *gin.Context) {
	if err := h.svc.CancelBudget(c.Request.Context(), c.Param("id")); err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) deleteBudget(c *gin.Context) {
	if err := h.svc.DeleteBudget(c.Request.Context(), c.Param("id")); err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.Status(http.StatusNoContent)
}

type scheduleDTO struct {
	Name          string  `json:"name"`
	Frequency     string  `json:"frequency"`
	DayOfWeek     *int    `json:"day_of_week"`
	DayOfMonth    *int    `json:"day_of_month"`
	CoverageWeeks int     `json:"coverage_weeks"`
	SafetyPercent float64 `json:"safety_percent"`
	LookbackWeeks int     `json:"lookback_weeks"`
	Active        *bool   `json:"active"`
}

func (d scheduleDTO) toDomain() domain.BudgetSchedule {
	active := true
	if d.Active != nil {
		active = *d.Active
	}
	return domain.BudgetSchedule{
		Name:          d.Name,
		Frequency:     d.Frequency,
		DayOfWeek:     d.DayOfWeek,
		DayOfMonth:    d.DayOfMonth,
		CoverageWeeks: d.CoverageWeeks,
		SafetyPercent: d.SafetyPercent,
		LookbackWeeks: d.LookbackWeeks,
		Active:        active,
	}
}

func (h *Handler) listSchedules(c *gin.Context) {
	out, err := h.svc.ListSchedules(c.Request.Context())
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) createSchedule(c *gin.Context) {
	var in scheduleDTO
	if err := c.ShouldBindJSON(&in); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}
	out, err := h.svc.CreateSchedule(c.Request.Context(), in.toDomain())
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h *Handler) getSchedule(c *gin.Context) {
	out, err := h.svc.GetSchedule(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) updateSchedule(c *gin.Context) {
	var in scheduleDTO
	if err := c.ShouldBindJSON(&in); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}
	out, err := h.svc.UpdateSchedule(c.Request.Context(), c.Param("id"), in.toDomain())
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) deleteSchedule(c *gin.Context) {
	if err := h.svc.DeleteSchedule(c.Request.Context(), c.Param("id")); err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) runScheduleNow(c *gin.Context) {
	sch, err := h.svc.GetSchedule(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	out, err := h.svc.RunSchedule(h.withAuth(c), sch)
	if err != nil {
		httpserver.Error(c, status(err), err)
		return
	}
	c.JSON(http.StatusCreated, out)
}
