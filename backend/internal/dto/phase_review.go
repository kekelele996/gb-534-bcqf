package dto
import (
	"time"

	"fermentation-kinetics-deviation-analysis/backend/internal/model"
)

// PhaseReviewDecisionRequest records one reviewer conclusion for a single
// high-risk phase. Basis is mandatory for every decision; excluding a
// suspected cause additionally requires the concrete cause that was ruled out.
type PhaseReviewDecisionRequest struct {
	Phase         string `json:"phase" binding:"required,oneof=lag growth production harvest"`
	Decision      string `json:"decision" binding:"required,oneof=accepted excluded returned"`
	Basis         string `json:"basis" binding:"omitempty,max=1000"`
	ExcludedCause string `json:"excluded_cause" binding:"omitempty,max=1000"`
}

type PhaseReviewResponse struct {
	ID             uint      `json:"id"`
	AnalysisID     uint      `json:"analysis_id"`
	Phase          string    `json:"phase"`
	Decision       string    `json:"decision"`
	ExcludedCause  string    `json:"excluded_cause,omitempty"`
	Basis          string    `json:"basis"`
	ReviewedBy     uint      `json:"reviewed_by"`
	ReviewedByName string    `json:"reviewed_by_name"`
	ReviewedAt     time.Time `json:"reviewed_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PendingPhaseReview describes one high-risk phase that still blocks
// confirmation because it lacks a conclusive reviewer decision.
type PendingPhaseReview struct {
	Phase  string `json:"phase"`
	Reason string `json:"reason"`
}

func NewPhaseReviewResponse(review model.PhaseReview) PhaseReviewResponse {
	return PhaseReviewResponse{
		ID: review.ID, AnalysisID: review.AnalysisID, Phase: review.Phase,
		Decision: review.Decision, ExcludedCause: review.ExcludedCause, Basis: review.Basis,
		ReviewedBy: review.ReviewedBy, ReviewedByName: review.ReviewedByName,
		ReviewedAt: review.ReviewedAt, CreatedAt: review.CreatedAt, UpdatedAt: review.UpdatedAt,
	}
}
