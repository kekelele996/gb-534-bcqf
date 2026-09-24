package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"fermentation-kinetics-deviation-analysis/backend/internal/algorithm"
	"fermentation-kinetics-deviation-analysis/backend/internal/dto"
	"fermentation-kinetics-deviation-analysis/backend/internal/model"
	"fermentation-kinetics-deviation-analysis/backend/internal/repository"
	"fermentation-kinetics-deviation-analysis/backend/internal/timeseries"
	"fermentation-kinetics-deviation-analysis/backend/internal/util"
)

func newPhaseReviewAnalysisService(t *testing.T, deviation float64) (
	*DeviationAnalysisService, repository.AuditRepository, uint,
) {
	t.Helper()
	db := newTestDB(t)
	vesselRepo := repository.NewFermentationVesselRepository(db)
	recipeRepo := repository.NewCultureRecipeRepository(db)
	seriesRepo := repository.NewSensorSeriesRepository(db)
	analysisRepo := repository.NewDeviationAnalysisRepository(db)
	phaseReviewRepo := repository.NewPhaseReviewRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	vessel := model.FermentationVessel{
		VesselCode: "FV-PR", Name: "Phase review vessel", WorkingVolumeL: 100,
		SensorChannels: `["ph"]`, Location: "Lab", OwnerTeam: "Process",
		VesselState: "active", CommissionedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := vesselRepo.Create(context.Background(), &vessel); err != nil {
		t.Fatal(err)
	}
	boundaries, references, tolerances := testRecipeConfig(t)
	recipe := model.CultureRecipe{
		VesselID: vessel.ID, RecipeCode: "PR-A", Version: 1, Organism: "Test organism",
		TargetDurationH: 8, PhaseBoundariesJSON: string(boundaries), ReferenceCurvesJSON: string(references),
		ToleranceProfileJSON: string(tolerances), RecipeState: "published",
		CreatedBy: 8, CreatedByName: "scientist", CreatedAt: now, UpdatedAt: now,
	}
	if err := recipeRepo.Create(context.Background(), &recipe); err != nil {
		t.Fatal(err)
	}
	points := make([]timeseries.Point, 0, 9)
	for hour := 0; hour <= 8; hour++ {
		value := 7 - float64(hour)*0.05 - deviation
		valueCopy := value
		points = append(points, timeseries.Point{
			Timestamp: now.Add(time.Duration(hour) * time.Hour), Values: map[string]*float64{"ph": &valueCopy},
		})
	}
	pointsJSON, err := timeseries.EncodePoints(points)
	if err != nil {
		t.Fatal(err)
	}
	series := model.SensorSeries{
		VesselID: vessel.ID, RecipeID: recipe.ID, RunCode: "RUN-PR", Channel: "ph",
		SampleIntervalS: 3600, PointsJSON: pointsJSON, StartedAt: now, EndedAt: now.Add(8 * time.Hour),
		SourceChecksum: util.HashString(pointsJSON), SeriesState: "ready", QualitySummary: `{"valid":true}`,
		NormalizationJSON: `{"method":"median_iqr"}`, ImportedBy: 9, ImportedByName: "analyst",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := seriesRepo.Create(context.Background(), &series); err != nil {
		t.Fatal(err)
	}
	svc := NewDeviationAnalysisService(
		analysisRepo, phaseReviewRepo, recipeRepo, seriesRepo, auditRepo, algorithm.NewEvaluator(),
	)
	return svc, auditRepo, series.ID
}

func phaseReviewActors() (util.Actor, util.Actor, util.Actor) {
	initiator := util.Actor{UserID: 9, Username: "analyst", Role: "data_analyst", RequestID: "req-init"}
	first := util.Actor{UserID: 10, Username: "reviewer", Role: "reviewer", RequestID: "req-first"}
	second := util.Actor{UserID: 11, Username: "reviewer-two", Role: "reviewer", RequestID: "req-second"}
	return initiator, first, second
}

func causeForPhase(analysis dto.DeviationAnalysisResponse, phase string) string {
	var causes []string
	if err := json.Unmarshal(analysis.SuspectedCausesJSON, &causes); err != nil {
		return ""
	}
	for _, cause := range causes {
		if strings.HasPrefix(cause, phase+" ") {
			return cause
		}
	}
	return ""
}

func TestPhaseReviewGateOwnershipAndReturnCycle(t *testing.T) {
	ctx := context.Background()
	svc, audits, seriesID := newPhaseReviewAnalysisService(t, 3.0)
	initiator, first, second := phaseReviewActors()
	analysis, reused, err := svc.Run(ctx, dto.RunDeviationAnalysisRequest{SensorSeriesID: seriesID}, "idem-pr", initiator)
	if err != nil || reused {
		t.Fatalf("run reused=%v err=%v", reused, err)
	}
	if len(analysis.PendingPhaseReviews) == 0 {
		t.Fatal("expected high-risk phases for a strongly deviating series")
	}
	if _, err := svc.Transition(ctx, analysis.ID, dto.DeviationAnalysisTransitionRequest{
		ToState: "reviewed", Comment: "Start review",
	}, first); err != nil {
		t.Fatalf("review transition: %v", err)
	}
	// Confirmation is blocked while high-risk phases lack conclusions.
	_, err = svc.Transition(ctx, analysis.ID, dto.DeviationAnalysisTransitionRequest{ToState: "confirmed"}, first)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.Code != util.CodeStateTransition {
		t.Fatalf("confirm without phase reviews error=%v, want state transition block", err)
	}
	blocked, err := svc.Get(ctx, analysis.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.PendingPhaseReviews) != len(analysis.PendingPhaseReviews) {
		t.Fatalf("pending=%d want %d listed blockers", len(blocked.PendingPhaseReviews), len(analysis.PendingPhaseReviews))
	}
	// Basis is mandatory for every decision.
	if _, err := svc.SubmitPhaseReview(ctx, analysis.ID, dto.PhaseReviewDecisionRequest{
		Phase: blocked.PendingPhaseReviews[0].Phase, Decision: "accepted",
	}, first); !errors.As(err, &appErr) || appErr.Status != 422 {
		t.Fatalf("missing basis error=%v, want 422", err)
	}
	// Exclusion requires a concrete cause selected from the reported causes.
	// The evaluator emits one cause per channel, keyed by the last flagged
	// phase, so locate the high-risk phase that actually carries a cause.
	excludedPhase := ""
	for _, item := range blocked.PendingPhaseReviews {
		if causeForPhase(blocked, item.Phase) != "" {
			excludedPhase = item.Phase
			break
		}
	}
	if excludedPhase == "" {
		t.Fatal("expected at least one high-risk phase to carry a suspected cause")
	}
	if _, err := svc.SubmitPhaseReview(ctx, analysis.ID, dto.PhaseReviewDecisionRequest{
		Phase: excludedPhase, Decision: "excluded", Basis: "Offline sampling contradicts it.",
	}, first); !errors.As(err, &appErr) || appErr.Status != 422 {
		t.Fatalf("exclude without cause error=%v, want 422", err)
	}
	cause := causeForPhase(blocked, excludedPhase)
	if cause == "" {
		t.Fatalf("no suspected cause reported for phase %s", excludedPhase)
	}
	if _, err := svc.SubmitPhaseReview(ctx, analysis.ID, dto.PhaseReviewDecisionRequest{
		Phase: excludedPhase, Decision: "excluded", Basis: "Offline sampling contradicts it.",
		ExcludedCause: "unrelated made-up cause",
	}, first); !errors.As(err, &appErr) || appErr.Status != 422 {
		t.Fatalf("exclude with foreign cause error=%v, want 422", err)
	}
	if _, err := svc.SubmitPhaseReview(ctx, analysis.ID, dto.PhaseReviewDecisionRequest{
		Phase: excludedPhase, Decision: "excluded", Basis: "Offline sampling contradicts it.",
		ExcludedCause: cause,
	}, first); err != nil {
		t.Fatalf("valid exclusion: %v", err)
	}
	// A second reviewer cannot touch a phase already claimed by the first.
	if _, err := svc.SubmitPhaseReview(ctx, analysis.ID, dto.PhaseReviewDecisionRequest{
		Phase: excludedPhase, Decision: "accepted", Basis: "I disagree",
	}, second); !errors.As(err, &appErr) || appErr.Code != util.CodeConflict {
		t.Fatalf("second reviewer override error=%v, want conflict", err)
	}
	// Low-risk phases cannot be reviewed; this is covered separately by
	// TestPhaseReviewRejectsLowRiskPhase because this fixture flags all phases.
	detail, err := svc.Get(ctx, analysis.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Resolve every remaining phase. Return one first to verify the analysis
	// moves to investigating and confirmation stays blocked until resolved.
	returnedPhase := ""
	for _, item := range detail.PendingPhaseReviews {
		if item.Phase == excludedPhase {
			continue
		}
		if returnedPhase == "" {
			returnedPhase = item.Phase
			if _, err := svc.SubmitPhaseReview(ctx, analysis.ID, dto.PhaseReviewDecisionRequest{
				Phase: item.Phase, Decision: "returned", Basis: "Need batch records.",
			}, first); err != nil {
				t.Fatalf("return phase %s: %v", item.Phase, err)
			}
			continue
		}
		if _, err := svc.SubmitPhaseReview(ctx, analysis.ID, dto.PhaseReviewDecisionRequest{
			Phase: item.Phase, Decision: "accepted", Basis: "Aligned curve and offline samples agree.",
		}, first); err != nil {
			t.Fatalf("accept phase %s: %v", item.Phase, err)
		}
	}
	afterReturn, err := svc.Get(ctx, analysis.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterReturn.AnalysisState != "investigating" {
		t.Fatalf("state after return=%s, want investigating", afterReturn.AnalysisState)
	}
	if _, err := svc.Transition(ctx, analysis.ID, dto.DeviationAnalysisTransitionRequest{ToState: "confirmed"}, first); err == nil {
		t.Fatal("confirmation succeeded while a returned phase is unresolved")
	}
	// Only the first reviewer resolves their own returned phase; then the
	// analysis re-enters reviewed and confirmation succeeds.
	if _, err := svc.SubmitPhaseReview(ctx, analysis.ID, dto.PhaseReviewDecisionRequest{
		Phase: returnedPhase, Decision: "accepted", Basis: "Batch records confirm the sensor evidence.",
	}, first); err != nil {
		t.Fatalf("resolve returned phase: %v", err)
	}
	if _, err := svc.Transition(ctx, analysis.ID, dto.DeviationAnalysisTransitionRequest{ToState: "reviewed"}, first); err != nil {
		t.Fatalf("re-review after investigation: %v", err)
	}
	confirmed, err := svc.Transition(ctx, analysis.ID, dto.DeviationAnalysisTransitionRequest{
		ToState: "confirmed", Comment: "All high-risk phases resolved.",
	}, first)
	if err != nil {
		t.Fatalf("confirm after resolution: %v", err)
	}
	if confirmed.AnalysisState != "confirmed" || len(confirmed.PendingPhaseReviews) != 0 {
		t.Fatalf("confirmed=%s pending=%d", confirmed.AnalysisState, len(confirmed.PendingPhaseReviews))
	}
	if len(confirmed.PhaseReviews) != len(blocked.PendingPhaseReviews) {
		t.Fatalf("phase reviews=%d want %d", len(confirmed.PhaseReviews), len(blocked.PendingPhaseReviews))
	}
	for _, review := range confirmed.PhaseReviews {
		if review.ReviewedByName != "reviewer" || review.Basis == "" || review.ReviewedAt.IsZero() {
			t.Fatalf("phase review %s is missing operator/time/basis", review.Phase)
		}
	}
	logs, total, err := audits.List(ctx, repository.AuditQuery{
		EntityType: "deviation_analysis", Action: "phase_decision", Page: 1, PageSize: 100,
	})
	if err != nil || total == 0 {
		t.Fatalf("phase decision audit logs total=%d err=%v", total, err)
	}
	if len(logs) < len(confirmed.PhaseReviews)+1 {
		t.Fatalf("expected an audit row per decision change including the return resolution, got %d", len(logs))
	}
}

func TestPhaseReviewRequiresReviewedAnalysis(t *testing.T) {
	ctx := context.Background()
	svc, _, seriesID := newPhaseReviewAnalysisService(t, 3.0)
	initiator, first, _ := phaseReviewActors()
	analysis, _, err := svc.Run(ctx, dto.RunDeviationAnalysisRequest{SensorSeriesID: seriesID}, "idem-state", initiator)
	if err != nil {
		t.Fatal(err)
	}
	phase := analysis.PendingPhaseReviews[0].Phase
	_, err = svc.SubmitPhaseReview(ctx, analysis.ID, dto.PhaseReviewDecisionRequest{
		Phase: phase, Decision: "accepted", Basis: "Too early",
	}, first)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.Code != util.CodeStateTransition {
		t.Fatalf("phase review before reviewed state error=%v, want state transition conflict", err)
	}
}

func TestPhaseReviewRejectsLowRiskPhase(t *testing.T) {
	ctx := context.Background()
	svc, _, seriesID := newPhaseReviewAnalysisService(t, 0)
	initiator, first, _ := phaseReviewActors()
	analysis, _, err := svc.Run(ctx, dto.RunDeviationAnalysisRequest{SensorSeriesID: seriesID}, "idem-low", initiator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, analysis.ID, dto.DeviationAnalysisTransitionRequest{
		ToState: "reviewed", Comment: "Start review",
	}, first); err != nil {
		t.Fatalf("review transition: %v", err)
	}
	_, err = svc.SubmitPhaseReview(ctx, analysis.ID, dto.PhaseReviewDecisionRequest{
		Phase: "lag", Decision: "accepted", Basis: "Looks fine but below threshold",
	}, first)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.Status != 422 {
		t.Fatalf("low-risk phase review error=%v, want 422", err)
	}
	// With no high-risk phases nothing blocks confirmation.
	confirmed, err := svc.Transition(ctx, analysis.ID, dto.DeviationAnalysisTransitionRequest{ToState: "confirmed"}, first)
	if err != nil {
		t.Fatalf("confirm analysis without high-risk phases: %v", err)
	}
	if confirmed.AnalysisState != "confirmed" {
		t.Fatalf("state=%s, want confirmed", confirmed.AnalysisState)
	}
}
