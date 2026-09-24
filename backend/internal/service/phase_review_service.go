package service
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"fermentation-kinetics-deviation-analysis/backend/internal/algorithm"
	"fermentation-kinetics-deviation-analysis/backend/internal/constants"
	"fermentation-kinetics-deviation-analysis/backend/internal/dto"
	"fermentation-kinetics-deviation-analysis/backend/internal/model"
	"fermentation-kinetics-deviation-analysis/backend/internal/util"

	"gorm.io/gorm"
)

// SubmitPhaseReview records one reviewer conclusion for a single high-risk
// phase. The first reviewer to submit claims the phase; later submissions from
// anyone else are rejected. The same reviewer may change their conclusion, and
// marking a phase as returned moves the analysis back into investigation.
func (s *DeviationAnalysisService) SubmitPhaseReview(
	ctx context.Context, id uint, request dto.PhaseReviewDecisionRequest, actor util.Actor,
) (dto.DeviationAnalysisResponse, error) {
	basis := strings.TrimSpace(request.Basis)
	if basis == "" {
		return dto.DeviationAnalysisResponse{}, util.NewError(http.StatusUnprocessableEntity, util.CodeValidation,
			"basis is required for accepting, excluding, or returning a phase")
	}
	decision := constants.PhaseReviewDecision(request.Decision)
	excludedCause := strings.TrimSpace(request.ExcludedCause)
	if decision == constants.PhaseDecisionExcluded && excludedCause == "" {
		return dto.DeviationAnalysisResponse{}, util.NewError(http.StatusUnprocessableEntity, util.CodeValidation,
			"a concrete suspected cause must be selected when excluding a cause")
	}
	if decision != constants.PhaseDecisionExcluded {
		excludedCause = ""
	}
	analysis, err := s.analyses.GetByID(ctx, id, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.DeviationAnalysisResponse{}, util.NotFound("deviation analysis")
		}
		return dto.DeviationAnalysisResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load deviation analysis", err)
	}
	evidence, err := decodePhaseEvidence(analysis.PhaseScoresJSON)
	if err != nil {
		return dto.DeviationAnalysisResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "frozen phase evidence is invalid", err)
	}
	score, found := phaseEvidenceFor(evidence, request.Phase)
	if !found {
		return dto.DeviationAnalysisResponse{}, util.NewError(http.StatusUnprocessableEntity, util.CodeValidation,
			"phase "+request.Phase+" is not part of this analysis")
	}
	if score.WeightedDeviation < constants.HighRiskPhaseThreshold {
		return dto.DeviationAnalysisResponse{}, util.NewError(http.StatusUnprocessableEntity, util.CodeValidation,
			fmt.Sprintf("phase %s weighted deviation %.1f%% is below the 20%% review threshold",
				request.Phase, score.WeightedDeviation*100))
	}
	if decision == constants.PhaseDecisionExcluded {
		causes, causeErr := s.eligibleCausesForPhase(ctx, analysis, request.Phase)
		if causeErr != nil {
			return dto.DeviationAnalysisResponse{}, causeErr
		}
		if !containsString(causes, excludedCause) {
			return dto.DeviationAnalysisResponse{}, util.NewError(http.StatusUnprocessableEntity, util.CodeValidation,
				"the excluded cause must be one of the suspected causes reported for phase "+request.Phase)
		}
	}
	state := constants.AnalysisState(analysis.AnalysisState)
	if state != constants.AnalysisReviewed && state != constants.AnalysisInvestigating {
		return dto.DeviationAnalysisResponse{}, util.NewError(http.StatusConflict, util.CodeStateTransition,
			"mark the analysis as reviewed before processing individual high-risk phases")
	}
	reviews, err := s.phaseReviews.ListByAnalysis(ctx, id)
	if err != nil {
		return dto.DeviationAnalysisResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load phase reviews", err)
	}
	var existing *model.PhaseReview
	for i := range reviews {
		if reviews[i].Phase == request.Phase {
			existing = &reviews[i]
			break
		}
	}
	now := s.now()
	review := model.PhaseReview{
		AnalysisID: id, Phase: request.Phase, Decision: request.Decision,
		ExcludedCause: excludedCause, Basis: basis,
		ReviewedBy: actor.UserID, ReviewedByName: actor.Username, ReviewedAt: now,
	}
	var before any = map[string]any{"phase": request.Phase, "decision": nil}
	if existing == nil {
		review.CreatedAt = now
		claimed, claimErr := s.phaseReviews.CreateIfAbsent(ctx, &review)
		if claimErr != nil {
			return dto.DeviationAnalysisResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to accept phase review", claimErr)
		}
		if !claimed {
			return dto.DeviationAnalysisResponse{}, util.NewError(http.StatusConflict, util.CodeConflict,
				"phase "+request.Phase+" has already been accepted by another reviewer")
		}
	} else {
		before = *existing
		if existing.ReviewedBy != actor.UserID {
			return dto.DeviationAnalysisResponse{}, util.NewError(http.StatusConflict, util.CodeConflict,
				fmt.Sprintf("phase %s is already being handled by %s; only the first reviewer may change its conclusion",
					request.Phase, existing.ReviewedByName))
		}
		review.ID = existing.ID
		review.CreatedAt = existing.CreatedAt
		updated, updateErr := s.phaseReviews.UpdateOwned(ctx, &review)
		if updateErr != nil {
			return dto.DeviationAnalysisResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to update phase review", updateErr)
		}
		if !updated {
			return dto.DeviationAnalysisResponse{}, util.NewError(http.StatusConflict, util.CodeConflict,
				"phase review ownership changed concurrently")
		}
	}
	// Returning a phase for investigation moves the whole analysis back so that
	// confirmation stays blocked until the investigation resolves the phase.
	if decision == constants.PhaseDecisionReturned && state == constants.AnalysisReviewed {
		changed, transitionErr := s.analyses.Transition(ctx, id, string(constants.AnalysisReviewed),
			string(constants.AnalysisInvestigating), nil)
		if transitionErr != nil {
			return dto.DeviationAnalysisResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to return analysis to investigation", transitionErr)
		}
		if !changed {
			return dto.DeviationAnalysisResponse{}, util.NewError(http.StatusConflict, util.CodeConflict, "analysis state changed concurrently")
		}
	}
	if err := recordAudit(ctx, s.audits, actor, "deviation_analysis", id, "phase_decision", before, review,
		analysis.InputHash, analysis.AlgorithmVersion, 0); err != nil {
		return dto.DeviationAnalysisResponse{}, err
	}
	return s.Get(ctx, id)
}

func (s *DeviationAnalysisService) eligibleCausesForPhase(
	ctx context.Context, analysis model.DeviationAnalysis, phase string,
) ([]string, error) {
	var causes []string
	if err := json.Unmarshal([]byte(analysis.SuspectedCausesJSON), &causes); err != nil {
		return nil, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "frozen suspected causes are invalid", err)
	}
	prefix := phase + " "
	eligible := make([]string, 0, len(causes))
	for _, cause := range causes {
		if strings.HasPrefix(cause, prefix) {
			eligible = append(eligible, cause)
		}
	}
	return eligible, nil
}

func (s *DeviationAnalysisService) pendingForConfirmation(
	ctx context.Context, analysis model.DeviationAnalysis,
) ([]dto.PendingPhaseReview, error) {
	reviews, err := s.phaseReviews.ListByAnalysis(ctx, analysis.ID)
	if err != nil {
		return nil, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load phase reviews", err)
	}
	byPhase := make(map[string]dto.PhaseReviewResponse, len(reviews))
	for _, review := range reviews {
		byPhase[review.Phase] = dto.NewPhaseReviewResponse(review)
	}
	return PendingHighRiskPhases(analysis.PhaseScoresJSON, byPhase)
}

// PendingHighRiskPhases lists every phase whose weighted deviation reaches the
// review threshold but has no conclusive conclusion (missing, or returned for
// investigation). It is a pure function so the confirmation gate is testable
// independently of the database.
func PendingHighRiskPhases(
	phaseScoresJSON string, reviews map[string]dto.PhaseReviewResponse,
) ([]dto.PendingPhaseReview, error) {
	evidence, err := decodePhaseEvidence(phaseScoresJSON)
	if err != nil {
		return nil, err
	}
	pending := []dto.PendingPhaseReview{}
	for _, score := range evidence {
		if score.WeightedDeviation < constants.HighRiskPhaseThreshold {
			continue
		}
		review, handled := reviews[score.Phase]
		switch {
		case !handled:
			pending = append(pending, dto.PendingPhaseReview{
				Phase:  score.Phase,
				Reason: fmt.Sprintf("weighted deviation %.1f%% has no reviewer conclusion", score.WeightedDeviation*100),
			})
		case review.Decision == string(constants.PhaseDecisionReturned):
			pending = append(pending, dto.PendingPhaseReview{
				Phase:  score.Phase,
				Reason: "phase was returned for investigation and is still unresolved",
			})
		}
	}
	return pending, nil
}

func decodePhaseEvidence(raw string) ([]algorithm.PhaseEvidence, error) {
	var evidence []algorithm.PhaseEvidence
	if err := json.Unmarshal([]byte(raw), &evidence); err != nil {
		return nil, fmt.Errorf("decode frozen phase evidence: %w", err)
	}
	return evidence, nil
}

func phaseEvidenceFor(evidence []algorithm.PhaseEvidence, phase string) (algorithm.PhaseEvidence, bool) {
	for _, item := range evidence {
		if item.Phase == phase {
			return item, true
		}
	}
	return algorithm.PhaseEvidence{}, false
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
