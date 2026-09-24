package constants
// HighRiskPhaseThreshold is the weighted deviation (inclusive) at or above
// which a phase must receive an individual reviewer decision before the
// analysis may be confirmed.
const HighRiskPhaseThreshold = 0.20
type PhaseReviewDecision string
const (
	PhaseReviewAccepted PhaseReviewDecision = "accepted"
	PhaseReviewExcluded PhaseReviewDecision = "excluded"
	PhaseReviewReturned PhaseReviewDecision = "returned"
)
func (d PhaseReviewDecision) Valid() bool {
	switch d {
	case PhaseReviewAccepted, PhaseReviewExcluded, PhaseReviewReturned:
		return true
	default:
		return false
	}
}
func PhaseReviewDecisionValues() []string {
	return []string{string(PhaseReviewAccepted), string(PhaseReviewExcluded), string(PhaseReviewReturned)}
}
// PhaseReviewAction names the audit actions emitted by per-phase review
// decisions. They are stored in audit_logs.action and also surfaced through
// the analysis detail response.
const (
	PhaseReviewActionAccepted = "phase_review_accepted"
	PhaseReviewActionExcluded = "phase_review_excluded"
	PhaseReviewActionReturned = "phase_review_returned"
)
func PhaseReviewAction(decision PhaseReviewDecision) string {
	switch decision {
	case PhaseReviewAccepted:
		return PhaseReviewActionAccepted
	case PhaseReviewExcluded:
		return PhaseReviewActionExcluded
	case PhaseReviewReturned:
		return PhaseReviewActionReturned
	default:
		return ""
	}
}
