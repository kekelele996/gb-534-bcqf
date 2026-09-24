package model
import "time"

// PhaseReview stores the per-phase conclusion a reviewer must record for every
// high-risk phase (weighted deviation at or above the review threshold). One
// row exists per (analysis, phase); the first reviewer to submit claims the
// phase and remains the only person who can change its conclusion.
type PhaseReview struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	AnalysisID     uint      `gorm:"not null;uniqueIndex:idx_phase_review_analysis_phase,priority:1;index" json:"analysis_id"`
	Phase          string    `gorm:"size:24;not null;uniqueIndex:idx_phase_review_analysis_phase,priority:2" json:"phase"`
	Decision       string    `gorm:"size:24;not null;index" json:"decision"`
	ExcludedCause  string    `gorm:"type:text" json:"excluded_cause,omitempty"`
	Basis          string    `gorm:"type:text;not null" json:"basis"`
	ReviewedBy     uint      `gorm:"not null;index" json:"reviewed_by"`
	ReviewedByName string    `gorm:"size:80;not null" json:"reviewed_by_name"`
	ReviewedAt     time.Time `gorm:"not null" json:"reviewed_at"`
	CreatedAt      time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt      time.Time `gorm:"not null" json:"updated_at"`
}

func (PhaseReview) TableName() string { return "phase_reviews" }
