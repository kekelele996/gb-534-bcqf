package dto
import (
	"encoding/json"
	"fermentation-kinetics-deviation-analysis/backend/internal/model"
	"time"
)
type RunDeviationAnalysisRequest struct {
	SensorSeriesID uint `json:"sensor_series_id" binding:"required"`
}
type DeviationAnalysisTransitionRequest struct {
	ToState string `json:"to_state" binding:"required,oneof=reviewed confirmed investigating voided"`
	Comment string `json:"comment" binding:"omitempty,max=1000"`
}
// PhaseReviewRequest is the per-phase reviewer decision. accepted only needs a
// phase; excluded additionally requires a concrete cause; returned and
// excluded both require a written rationale.
type PhaseReviewRequest struct {
	Phase     string `json:"phase" binding:"required,oneof=lag growth production harvest"`
	Decision  string `json:"decision" binding:"required,oneof=accepted excluded returned"`
	Cause     string `json:"cause" binding:"omitempty,max=500"`
	Rationale string `json:"rationale" binding:"omitempty,max=1000"`
}
type PhaseReviewResponse struct {
	Phase          string    `json:"phase"`
	Decision       string    `json:"decision"`
	Cause          string    `json:"cause,omitempty"`
	Rationale      string    `json:"rationale"`
	ReviewedBy     uint      `json:"reviewed_by"`
	ReviewedByName string    `json:"reviewed_by_name"`
	ReviewedAt     time.Time `json:"reviewed_at"`
}
// PhaseReviewEvent is one historical decision change for a phase, sourced from
// the audit trail so every change keeps operator and timestamp.
type PhaseReviewEvent struct {
	Phase          string    `json:"phase"`
	Action         string    `json:"action"`
	Decision       string    `json:"decision"`
	Cause          string    `json:"cause,omitempty"`
	Rationale      string    `json:"rationale"`
	ActorID        uint      `json:"actor_id"`
	ActorName      string    `json:"actor_name"`
	ActorRole      string    `json:"actor_role"`
	RequestID      string    `json:"request_id"`
	CreatedAt      time.Time `json:"created_at"`
}
type DeviationAnalysisQuery struct {
	SensorSeriesID, RecipeID uint
	State, Level, Initiator  string
	Page, PageSize           int
}
type DeviationAnalysisResponse struct {
	ID                   uint                  `json:"id"`
	SensorSeriesID       uint                  `json:"sensor_series_id"`
	RecipeID             uint                  `json:"recipe_id"`
	RecipeVersion        int                   `json:"recipe_version"`
	AlgorithmVersion     string                `json:"algorithm_version"`
	InputHash            string                `json:"input_hash"`
	PhaseScoresJSON      json.RawMessage       `json:"phase_scores_json"`
	DeviationLevel       string                `json:"deviation_level"`
	AlignedCurveJSON     json.RawMessage       `json:"aligned_curve_json"`
	SuspectedCausesJSON  json.RawMessage       `json:"suspected_causes_json"`
	AnalysisState        string                `json:"analysis_state"`
	Explanation          string                `json:"explanation"`
	AnalyzedAt           time.Time             `json:"analyzed_at"`
	InitiatedBy          uint                  `json:"initiated_by"`
	InitiatedByName      string                `json:"initiated_by_name"`
	ReviewedBy           *uint                 `json:"reviewed_by,omitempty"`
	ReviewedByName       string                `json:"reviewed_by_name,omitempty"`
	DurationMilliseconds int64                 `json:"duration_milliseconds"`
	FailureReason        string                `json:"failure_reason,omitempty"`
	ReviewComment        string                `json:"review_comment,omitempty"`
	ReplayVerified       *bool                 `json:"replay_verified,omitempty"`
	SensorSeries         *SensorSeriesResponse `json:"sensor_series,omitempty"`
	// PhaseReviews carries current per-phase decisions keyed by phase.
	PhaseReviews map[string]PhaseReviewResponse `json:"phase_reviews"`
	// PhaseReviewEvents is the chronological audit trail of phase decisions.
	PhaseReviewEvents []PhaseReviewEvent `json:"phase_review_events"`
	// PhaseCandidateCauses maps phase to the concrete rule-hit causes the
	// reviewer may select when excluding a suspected cause.
	PhaseCandidateCauses map[string][]string `json:"phase_candidate_causes"`
	// PendingReviewPhases lists high-risk phases without a terminal decision;
	// returned phases are also listed so confirmation can be blocked.
	PendingReviewPhases []string `json:"pending_review_phases"`
	// HighRiskPhases lists phases whose weighted deviation reaches 20%.
	HighRiskPhases []string `json:"high_risk_phases"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type DeviationAnalysisListResponse struct {
	Items []DeviationAnalysisResponse `json:"items"`
	Total int64                       `json:"total"`
	Page  int                         `json:"page"`
	Size  int                         `json:"page_size"`
}
func NewPhaseReviewResponse(review model.PhaseReview) PhaseReviewResponse {
	return PhaseReviewResponse{
		Phase: review.Phase, Decision: review.Decision, Cause: review.Cause,
		Rationale: review.Rationale, ReviewedBy: review.ReviewedBy,
		ReviewedByName: review.ReviewedByName, ReviewedAt: review.ReviewedAt,
	}
}
func NewDeviationAnalysisResponse(analysis model.DeviationAnalysis) DeviationAnalysisResponse {
	response := DeviationAnalysisResponse{
		ID: analysis.ID, SensorSeriesID: analysis.SensorSeriesID, RecipeID: analysis.RecipeID,
		RecipeVersion: analysis.RecipeVersion, AlgorithmVersion: analysis.AlgorithmVersion,
		InputHash: analysis.InputHash, PhaseScoresJSON: rawJSON(analysis.PhaseScoresJSON),
		DeviationLevel: analysis.DeviationLevel, AlignedCurveJSON: rawJSON(analysis.AlignedCurveJSON),
		SuspectedCausesJSON: rawJSON(analysis.SuspectedCausesJSON), AnalysisState: analysis.AnalysisState,
		Explanation: analysis.Explanation, AnalyzedAt: analysis.AnalyzedAt,
		InitiatedBy: analysis.InitiatedBy, InitiatedByName: analysis.InitiatedByName,
		ReviewedBy: analysis.ReviewedBy, ReviewedByName: analysis.ReviewedByName,
		DurationMilliseconds: analysis.DurationMilliseconds, FailureReason: analysis.FailureReason,
		ReviewComment: analysis.ReviewComment, ReplayVerified: analysis.ReplayVerified,
		PhaseReviews: map[string]PhaseReviewResponse{}, PhaseReviewEvents: []PhaseReviewEvent{},
		PhaseCandidateCauses: map[string][]string{}, PendingReviewPhases: []string{},
		HighRiskPhases: []string{},
		CreatedAt: analysis.CreatedAt, UpdatedAt: analysis.UpdatedAt,
	}
	if analysis.SensorSeries.ID != 0 {
		s := NewSensorSeriesResponse(analysis.SensorSeries)
		response.SensorSeries = &s
	}
	for _, review := range analysis.PhaseReviews {
		response.PhaseReviews[review.Phase] = NewPhaseReviewResponse(review)
	}
	response.PhaseReviewEvents = NewPhaseReviewEvents(analysis.PhaseReviewLogs)
	response.HighRiskPhases = highRiskPhases(analysis.PhaseScoresJSON)
	response.PendingReviewPhases = pendingReviewPhases(response.HighRiskPhases, response.PhaseReviews)
	if causes := candidateCausesFromEvidence(analysis.PhaseScoresJSON); causes != nil {
		response.PhaseCandidateCauses = causes
	}
	return response
}