package model
import "time"
// PhaseReview records the per-phase review decision for phases whose weighted
// deviation reaches the high-risk threshold. The first reviewer to submit a
// decision claims the phase; later submissions from other reviewers are
// rejected. Every decision is also projected into audit_logs by the service.
type PhaseReview struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	AnalysisID     uint   `gorm:"not null;uniqueIndex:idx_phase_review_analysis_phase" json:"analysis_id"`
	Phase          string `gorm:"size:24;not null;uniqueIndex:idx_phase_review_analysis_phase" json:"phase"`
	Decision       string `gorm:"size:24;not null;index" json:"decision"`
	Cause          string `gorm:"type:text" json:"cause,omitempty"`
	Rationale      string `gorm:"type:text;not null" json:"rationale"`
	ReviewedBy     uint   `gorm:"not null;index" json:"reviewed_by"`
	ReviewedByName string `gorm:"size:80;not null" json:"reviewed_by_name"`
	ReviewedAt     time.Time `gorm:"not null" json:"reviewed_at"`
	CreatedAt      time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt      time.Time `gorm:"not null" json:"updated_at"`
}
func (PhaseReview) TableName() string { return "phase_reviews" }
