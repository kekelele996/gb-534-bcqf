package constants
// HighRiskPhaseThreshold marks a phase as high-risk when its weighted deviation
// reaches 20% or more; every such phase needs an explicit reviewer conclusion.
const HighRiskPhaseThreshold = 0.20

type PhaseReviewDecision string

const (
	// PhaseDecisionAccepted means the reviewer accepts the algorithmic evidence as-is.
	PhaseDecisionAccepted PhaseReviewDecision = "accepted"
	// PhaseDecisionExcluded means the reviewer excludes a concrete suspected cause.
	PhaseDecisionExcluded PhaseReviewDecision = "excluded"
	// PhaseDecisionReturned means the reviewer returns the phase for further investigation.
	PhaseDecisionReturned PhaseReviewDecision = "returned"
)

func (d PhaseReviewDecision) Valid() bool {
	switch d {
	case PhaseDecisionAccepted, PhaseDecisionExcluded, PhaseDecisionReturned:
		return true
	default:
		return false
	}
}

func PhaseReviewDecisionValues() []string {
	return []string{
		string(PhaseDecisionAccepted), string(PhaseDecisionExcluded), string(PhaseDecisionReturned),
	}
}
