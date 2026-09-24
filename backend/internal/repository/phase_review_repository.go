package repository
import (
	"context"
	"fmt"
	"time"

	"fermentation-kinetics-deviation-analysis/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PhaseReviewRepository interface {
	ListByAnalysis(ctx context.Context, analysisID uint) ([]model.PhaseReview, error)
	// CreateIfAbsent atomically claims a phase for the first reviewer.
	CreateIfAbsent(ctx context.Context, review *model.PhaseReview) (bool, error)
	// UpdateOwned applies a new conclusion only while the row is still owned by
	// the same reviewer, so concurrent submissions cannot overwrite each other.
	UpdateOwned(ctx context.Context, review *model.PhaseReview) (bool, error)
}

type phaseReviewRepository struct{ db *gorm.DB }

func NewPhaseReviewRepository(db *gorm.DB) PhaseReviewRepository {
	return &phaseReviewRepository{db: db}
}

func (r *phaseReviewRepository) ListByAnalysis(ctx context.Context, analysisID uint) ([]model.PhaseReview, error) {
	var reviews []model.PhaseReview
	if err := r.db.WithContext(ctx).Where("analysis_id = ?", analysisID).
		Order("updated_at DESC, id DESC").Find(&reviews).Error; err != nil {
		return nil, fmt.Errorf("list phase reviews for analysis %d: %w", analysisID, err)
	}
	return reviews, nil
}

func (r *phaseReviewRepository) CreateIfAbsent(ctx context.Context, review *model.PhaseReview) (bool, error) {
	review.UpdatedAt = review.ReviewedAt
	if review.CreatedAt.IsZero() {
		review.CreatedAt = review.ReviewedAt
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(review)
	if result.Error != nil {
		return false, fmt.Errorf("claim phase review for analysis %d phase %s: %w",
			review.AnalysisID, review.Phase, result.Error)
	}
	return result.RowsAffected == 1, nil
}

func (r *phaseReviewRepository) UpdateOwned(ctx context.Context, review *model.PhaseReview) (bool, error) {
	review.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&model.PhaseReview{}).
		Where("analysis_id = ? AND phase = ? AND reviewed_by = ?",
			review.AnalysisID, review.Phase, review.ReviewedBy).
		Updates(map[string]any{
			"decision":       review.Decision,
			"excluded_cause": review.ExcludedCause,
			"basis":          review.Basis,
			"reviewed_at":    review.ReviewedAt,
			"updated_at":     review.UpdatedAt,
		})
	if result.Error != nil {
		return false, fmt.Errorf("update phase review for analysis %d phase %s: %w",
			review.AnalysisID, review.Phase, result.Error)
	}
	return result.RowsAffected == 1, nil
}
