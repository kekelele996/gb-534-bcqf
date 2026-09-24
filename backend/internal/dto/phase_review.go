package dto
import (
	"encoding/json"
	"fermentation-kinetics-deviation-analysis/backend/internal/constants"
	"fermentation-kinetics-deviation-analysis/backend/internal/model"
)
type phaseEvidencePayload struct {
	Phase            string   `json:"phase"`
	WeightedDeviation float64 `json:"weighted_deviation"`
	SuspectedCauses  []string `json:"suspected_causes"`
}
type phaseReviewAuditPayload struct {
	Phase     string `json:"phase"`
	Decision  string `json:"decision"`
	Cause     string `json:"cause"`
	Rationale string `json:"rationale"`
}
func parsePhaseEvidence(raw string) []phaseEvidencePayload {
	if raw == "" {
		return nil
	}
	var evidence []phaseEvidencePayload
	if err := json.Unmarshal([]byte(raw), &evidence); err != nil {
		return nil
	}
	return evidence
}
// highRiskPhases returns phases whose weighted deviation reaches the 20%
// inclusive threshold, preserving stored phase order.
func highRiskPhases(phaseScoresJSON string) []string {
	evidence := parsePhaseEvidence(phaseScoresJSON)
	phases := make([]string, 0, len(evidence))
	for _, item := range evidence {
		if item.WeightedDeviation+1e-9 >= constants.HighRiskPhaseThreshold {
			phases = append(phases, item.Phase)
		}
	}
	return phases
}
// pendingReviewPhases lists high-risk phases that block confirmation: those
// with no current decision, or whose current decision is "returned".
func pendingReviewPhases(highRisk []string, current map[string]PhaseReviewResponse) []string {
	pending := make([]string, 0, len(highRisk))
	for _, phase := range highRisk {
		review, ok := current[phase]
		if !ok || review.Decision == string(constants.PhaseReviewReturned) {
			pending = append(pending, phase)
		}
	}
	return pending
}
// candidateCausesFromEvidence extracts per-phase rule-hit causes embedded by
// algorithm v1.1.0+. Returns nil for historical results without embedded
// causes so the service can re-derive them from the frozen snapshot.
func candidateCausesFromEvidence(phaseScoresJSON string) map[string][]string {
	evidence := parsePhaseEvidence(phaseScoresJSON)
	if len(evidence) == 0 {
		return nil
	}
	embedded := false
	causes := make(map[string][]string, len(evidence))
	for _, item := range evidence {
		if item.SuspectedCauses == nil {
			continue
		}
		embedded = true
		causes[item.Phase] = item.SuspectedCauses
	}
	if !embedded {
		return nil
	}
	return causes
}
// NewPhaseReviewEvents converts phase-review audit logs into chronological
// decision events for the analysis detail response.
func NewPhaseReviewEvents(logs []model.AuditLog) []PhaseReviewEvent {
	events := make([]PhaseReviewEvent, 0, len(logs))
	for _, log := range logs {
		var payload phaseReviewAuditPayload
		if err := json.Unmarshal([]byte(log.AfterSnapshot), &payload); err != nil || payload.Phase == "" {
			continue
		}
		decision := payload.Decision
		if decision == "" {
			decision = phaseReviewActionToDecision(log.Action)
		}
		events = append(events, PhaseReviewEvent{
			Phase: payload.Phase, Action: log.Action, Decision: decision,
			Cause: payload.Cause, Rationale: payload.Rationale,
			ActorID: log.ActorID, ActorName: log.ActorName, ActorRole: log.ActorRole,
			RequestID: log.RequestID, CreatedAt: log.CreatedAt,
		})
	}
	return events
}
func phaseReviewActionToDecision(action string) string {
	switch action {
	case constants.PhaseReviewActionAccepted:
		return string(constants.PhaseReviewAccepted)
	case constants.PhaseReviewActionExcluded:
		return string(constants.PhaseReviewExcluded)
	case constants.PhaseReviewActionReturned:
		return string(constants.PhaseReviewReturned)
	default:
		return ""
	}
}
