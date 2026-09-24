package handler
import (
	"net/http"
	"fermentation-kinetics-deviation-analysis/backend/internal/dto"
	"fermentation-kinetics-deviation-analysis/backend/internal/service"
	"fermentation-kinetics-deviation-analysis/backend/internal/util"
	"github.com/gin-gonic/gin"
)
type PhaseReviewHandler struct {
	service *service.DeviationAnalysisService
}
func NewPhaseReviewHandler(value *service.DeviationAnalysisService) *PhaseReviewHandler {
	return &PhaseReviewHandler{service: value}
}
// Submit handles POST /deviation-analyses/:id/phase-reviews — one per-phase
// reviewer decision (accept evidence, exclude a suspected cause, or return the
// phase for investigation).
func (h *PhaseReviewHandler) Submit(c *gin.Context) {
	id, err := util.ParseUintParam(c, "id")
	if err != nil {
		util.Fail(c, err)
		return
	}
	var request dto.PhaseReviewRequest
	if !bindJSON(c, &request) {
		return
	}
	result, serviceErr := h.service.ReviewPhase(c.Request.Context(), id, request, mustActor(c))
	respond(c, http.StatusOK, result, serviceErr)
}
