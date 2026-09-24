package repository
import (
	"context"
	"fmt"
	"time"
	"fermentation-kinetics-deviation-analysis/backend/internal/model"
	"gorm.io/gorm"
)
type PhaseReviewRepository interface {
	ListByAnalysis(ctx context.Context, analysisID uint) ([]model.PhaseReview, error)
	// UpsertDecision claims an unhandled phase for the first reviewer. It
	// returns (true, nil) when the decision was stored; rowsAffected==0 means
	// the phase was already claimed by another reviewer or the analysis
	// changed concurrently.
	UpsertDecision(ctx context.Context, review *model.PhaseReview) (bool, error)
}
type phaseReviewRepository struct{ db *gorm.DB }
func NewPhaseReviewRepository(db *gorm.DB) PhaseReviewRepository {
	return &phaseReviewRepository{db: db}
}
func (r *phaseReviewRepository) ListByAnalysis(ctx context.Context, analysisID uint) ([]model.PhaseReview, error) {
	var reviews []model.PhaseReview
	if err := r.db.WithContext(ctx).Where("analysis_id = ?", analysisID).
		Order("reviewed_at ASC, id ASC").Find(&reviews).Error; err != nil {
		return nil, fmt.Errorf("list phase reviews for analysis %d: %w", analysisID, err)
	}
	return reviews, nil
}
func (r *phaseReviewRepository) UpsertDecision(ctx context.Context, review *model.PhaseReview) (bool, error) {
	now := time.Now().UTC()
	if review.ReviewedAt.IsZero() {
		review.ReviewedAt = now
	}
	review.CreatedAt = now
	review.UpdatedAt = now
	result := r.db.WithContext(ctx).Exec(`
INSERT INTO phase_reviews
  (analysis_id, phase, decision, cause, rationale, reviewed_by, reviewed_by_name, reviewed_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (analysis_id, phase) DO UPDATE SET
  decision = EXCLUDED.decision,
  cause = EXCLUDED.cause,
  rationale = EXCLUDED.rationale,
  reviewed_by = CASE
    WHEN phase_reviews.reviewed_by = EXCLUDED.reviewed_by THEN EXCLUDED.reviewed_by
    ELSE phase_reviews.reviewed_by
  END,
  reviewed_by_name = CASE
    WHEN phase_reviews.reviewed_by = EXCLUDED.reviewed_by THEN EXCLUDED.reviewed_by_name
    ELSE phase_reviews.reviewed_by_name
  END,
  reviewed_at = CASE
    WHEN phase_reviews.reviewed_by = EXCLUDED.reviewed_by THEN EXCLUDED.reviewed_at
    ELSE phase_reviews.reviewed_at
  END,
  updated_at = CASE
    WHEN phase_reviews.reviewed_by = EXCLUDED.reviewed_by THEN EXCLUDED.updated_at
    ELSE phase_reviews.updated_at
  END
WHERE phase_reviews.reviewed_by = EXCLUDED.reviewed_by`,
		review.AnalysisID, review.Phase, review.Decision, review.Cause, review.Rationale,
		review.ReviewedBy, review.ReviewedByName, review.ReviewedAt, now, now,
	)
	if result.Error != nil {
		return false, fmt.Errorf("upsert phase review for analysis %d phase %s: %w", review.AnalysisID, review.Phase, result.Error)
	}
	return result.RowsAffected == 1, nil
}
