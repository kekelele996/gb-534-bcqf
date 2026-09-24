package service
import (
	"encoding/json"
	"fermentation-kinetics-deviation-analysis/backend/internal/constants"
	"fermentation-kinetics-deviation-analysis/backend/internal/model"
)
type phaseScorePayload struct {
	Phase             string   `json:"phase"`
	WeightedDeviation float64  `json:"weighted_deviation"`
	SuspectedCauses   []string `json:"suspected_causes"`
}
func parsePhaseScores(raw string) []phaseScorePayload {
	if raw == "" {
		return nil
	}
	var scores []phaseScorePayload
	if err := json.Unmarshal([]byte(raw), &scores); err != nil {
		return nil
	}
	return scores
}
// isHighRiskPhase reports whether the named phase reached the inclusive 20%
// weighted deviation threshold in the frozen analysis result.
func isHighRiskPhase(phaseScoresJSON, phase string) bool {
	for _, score := range parsePhaseScores(phaseScoresJSON) {
		if score.Phase == phase {
			return score.WeightedDeviation+1e-9 >= constants.HighRiskPhaseThreshold
		}
	}
	return false
}
// pendingHighRiskPhases returns high-risk phases that block confirmation:
// those with no current decision, or currently returned for investigation.
func pendingHighRiskPhases(phaseScoresJSON string, reviews []model.PhaseReview) []string {
	decisions := make(map[string]string, len(reviews))
	for _, review := range reviews {
		decisions[review.Phase] = review.Decision
	}
	pending := []string{}
	for _, score := range parsePhaseScores(phaseScoresJSON) {
		if score.WeightedDeviation+1e-9 < constants.HighRiskPhaseThreshold {
			continue
		}
		if decisions[score.Phase] == "" || decisions[score.Phase] == string(constants.PhaseReviewReturned) {
			pending = append(pending, score.Phase)
		}
	}
	return pending
}
// dtoEmbeddedCauses returns the per-phase causes embedded by algorithm
// v1.1.0+, or nil for historical results without embedded attribution.
func dtoEmbeddedCauses(phaseScoresJSON, phase string) []string {
	for _, score := range parsePhaseScores(phaseScoresJSON) {
		if score.Phase == phase {
			return score.SuspectedCauses
		}
	}
	return nil
}
