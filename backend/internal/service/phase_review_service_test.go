package service

import (
	"context"
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

func phaseReviewFixture(t *testing.T, deviation float64) (*DeviationAnalysisService, dto.DeviationAnalysisResponse) {
	t.Helper()
	db := newTestDB(t)
	vesselRepo := repository.NewFermentationVesselRepository(db)
	recipeRepo := repository.NewCultureRecipeRepository(db)
	seriesRepo := repository.NewSensorSeriesRepository(db)
	analysisRepo := repository.NewDeviationAnalysisRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	reviewRepo := repository.NewPhaseReviewRepository(db)
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	vessel := model.FermentationVessel{
		VesselCode: "FV-P1", Name: "Phase review vessel", WorkingVolumeL: 100,
		SensorChannels: `["ph"]`, Location: "Lab", OwnerTeam: "Process",
		VesselState: "active", CommissionedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := vesselRepo.Create(context.Background(), &vessel); err != nil {
		t.Fatal(err)
	}
	boundaries, references, tolerances := testRecipeConfig(t)
	recipe := model.CultureRecipe{
		VesselID: vessel.ID, RecipeCode: "PHASE-A", Version: 1, Organism: "Test organism",
		TargetDurationH: 8, PhaseBoundariesJSON: string(boundaries), ReferenceCurvesJSON: string(references),
		ToleranceProfileJSON: string(tolerances), RecipeState: "published",
		CreatedBy: 8, CreatedByName: "scientist", CreatedAt: now, UpdatedAt: now,
	}
	if err := recipeRepo.Create(context.Background(), &recipe); err != nil {
		t.Fatal(err)
	}
	points := make([]timeseries.Point, 0, 9)
	for hour := 0; hour <= 8; hour++ {
		value := 7.0 - float64(hour)*0.05 - deviation*float64(hour%2)
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
		VesselID: vessel.ID, RecipeID: recipe.ID, RunCode: "RUN-P1", Channel: "ph",
		SampleIntervalS: 3600, PointsJSON: pointsJSON, StartedAt: now, EndedAt: now.Add(8 * time.Hour),
		SourceChecksum: util.HashString(pointsJSON), SeriesState: "ready", QualitySummary: `{"valid":true}`,
		NormalizationJSON: `{"method":"median_iqr"}`, ImportedBy: 9, ImportedByName: "analyst",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := seriesRepo.Create(context.Background(), &series); err != nil {
		t.Fatal(err)
	}
	svc := NewDeviationAnalysisService(analysisRepo, recipeRepo, seriesRepo, auditRepo, reviewRepo, algorithm.NewEvaluator())
	initiator := util.Actor{UserID: 9, Username: "analyst", Role: "data_analyst", RequestID: "req-run"}
	analysis, reused, err := svc.Run(context.Background(), dto.RunDeviationAnalysisRequest{SensorSeriesID: series.ID}, "idem-phase", initiator)
	if err != nil || reused {
		t.Fatalf("run analysis reused=%v err=%v", reused, err)
	}
	return svc, analysis
}

func wantAppError(t *testing.T, err error, code util.ErrorCode) {
	t.Helper()
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.Code != code {
		t.Fatalf("err=%v, want error code %s", err, code)
	}
}

func TestHighRiskPhasesBlockConfirmationUntilResolved(t *testing.T) {
	svc, analysis := phaseReviewFixture(t, 1.5)
	if len(analysis.HighRiskPhases) == 0 {
		t.Fatal("fixture should contain at least one high-risk phase")
	}
	reviewer := util.Actor{UserID: 10, Username: "reviewer", Role: "reviewer", RequestID: "req-review"}
	// Move to reviewed; confirmation is then blocked while high-risk phases
	// lack conclusions.
	if _, err := svc.Transition(context.Background(), analysis.ID, dto.DeviationAnalysisTransitionRequest{
		ToState: "reviewed",
	}, reviewer); err != nil {
		t.Fatalf("mark reviewed: %v", err)
	}
	_, err := svc.Transition(context.Background(), analysis.ID, dto.DeviationAnalysisTransitionRequest{
		ToState: "confirmed",
	}, reviewer)
	wantAppError(t, err, util.CodePhaseReview)
	// Review each high-risk phase; accepted needs no rationale.
	for _, phase := range analysis.HighRiskPhases {
		if _, err := svc.ReviewPhase(context.Background(), analysis.ID, dto.PhaseReviewRequest{
			Phase: phase, Decision: "accepted",
		}, reviewer); err != nil {
			t.Fatalf("accept phase %s: %v", phase, err)
		}
	}
	detail, err := svc.Get(context.Background(), analysis.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.PendingReviewPhases) != 0 {
		t.Fatalf("pending phases after all accepted: %v", detail.PendingReviewPhases)
	}
	if _, err := svc.Transition(context.Background(), analysis.ID, dto.DeviationAnalysisTransitionRequest{
		ToState: "confirmed", Comment: "all phases resolved",
	}, reviewer); err != nil {
		t.Fatalf("confirm after resolution: %v", err)
	}
}

func TestReturnedAndExcludedRequireRationaleAndExclusionNeedsCause(t *testing.T) {
	svc, analysis := phaseReviewFixture(t, 1.5)
	reviewer := util.Actor{UserID: 10, Username: "reviewer", Role: "reviewer", RequestID: "req-r"}
	phase := analysis.HighRiskPhases[0]
	// Return without rationale is rejected.
	_, err := svc.ReviewPhase(context.Background(), analysis.ID, dto.PhaseReviewRequest{
		Phase: phase, Decision: "returned",
	}, reviewer)
	wantAppError(t, err, util.CodeValidation)
	// Return with rationale moves the analysis to investigating.
	returned, err := svc.ReviewPhase(context.Background(), analysis.ID, dto.PhaseReviewRequest{
		Phase: phase, Decision: "returned", Rationale: "Curve gap needs offline check.",
	}, reviewer)
	if err != nil {
		t.Fatalf("return phase: %v", err)
	}
	if returned.AnalysisState != "investigating" {
		t.Fatalf("state=%s, want investigating", returned.AnalysisState)
	}
	if !contains(returned.PendingReviewPhases, phase) {
		t.Fatalf("returned phase %s must remain pending: %v", phase, returned.PendingReviewPhases)
	}
	// Exclusion requires a concrete selected cause and a rationale.
	another := firstPhase(analysis.HighRiskPhases, phase)
	if another != "" {
		_, err = svc.ReviewPhase(context.Background(), analysis.ID, dto.PhaseReviewRequest{
			Phase: another, Decision: "excluded", Rationale: "sensor calibration evidence",
		}, reviewer)
		wantAppError(t, err, util.CodeValidation)
	}
}

func TestFirstReviewerClaimsThePhase(t *testing.T) {
	svc, analysis := phaseReviewFixture(t, 1.5)
	first := util.Actor{UserID: 10, Username: "reviewer", Role: "reviewer", RequestID: "req-first"}
	second := util.Actor{UserID: 11, Username: "scientist", Role: "process_scientist", RequestID: "req-second"}
	phase := analysis.HighRiskPhases[0]
	if _, err := svc.ReviewPhase(context.Background(), analysis.ID, dto.PhaseReviewRequest{
		Phase: phase, Decision: "accepted",
	}, first); err != nil {
		t.Fatalf("first reviewer accepts: %v", err)
	}
	_, err := svc.ReviewPhase(context.Background(), analysis.ID, dto.PhaseReviewRequest{
		Phase: phase, Decision: "accepted",
	}, second)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.Code != util.CodePhaseClaimed || !strings.Contains(appErr.Message, "reviewer") {
		t.Fatalf("second reviewer err=%v, want claimed by reviewer", err)
	}
	// The initiator can never review a phase.
	initiator := util.Actor{UserID: 9, Username: "analyst", Role: "data_analyst", RequestID: "req-self"}
	_, err = svc.ReviewPhase(context.Background(), analysis.ID, dto.PhaseReviewRequest{
		Phase: phase, Decision: "accepted",
	}, initiator)
	wantAppError(t, err, util.CodeReviewerConflict)
	// The owner may revise their own decision.
	detail, err := svc.ReviewPhase(context.Background(), analysis.ID, dto.PhaseReviewRequest{
		Phase: phase, Decision: "accepted", Rationale: "Rechecked alignment.",
	}, first)
	if err != nil {
		t.Fatalf("owner revises decision: %v", err)
	}
	review := detail.PhaseReviews[phase]
	if review.ReviewedByName != "reviewer" || review.Rationale != "Rechecked alignment." {
		t.Fatalf("revision not stored: %+v", review)
	}
	if len(detail.PhaseReviewEvents) < 2 {
		t.Fatalf("expected audit events for every change, got %d", len(detail.PhaseReviewEvents))
	}
	for _, event := range detail.PhaseReviewEvents {
		if event.ActorName == "" || event.CreatedAt.IsZero() {
			t.Fatalf("event missing operator or time: %+v", event)
		}
	}
}

func TestLowRiskPhaseCannotBeReviewed(t *testing.T) {
	svc, analysis := phaseReviewFixture(t, 0)
	reviewer := util.Actor{UserID: 10, Username: "reviewer", Role: "reviewer", RequestID: "req-low"}
	if len(analysis.HighRiskPhases) != 0 {
		t.Fatalf("zero-deviation fixture must have no high-risk phases, got %v", analysis.HighRiskPhases)
	}
	_, err := svc.ReviewPhase(context.Background(), analysis.ID, dto.PhaseReviewRequest{
		Phase: "lag", Decision: "accepted",
	}, reviewer)
	wantAppError(t, err, util.CodeValidation)
}

func TestFirstReviewerClaimIsAtomic(t *testing.T) {
	db := newTestDB(t)
	reviewRepo := repository.NewPhaseReviewRepository(db)
	first := model.PhaseReview{
		AnalysisID: 1, Phase: "lag", Decision: "accepted", Rationale: "",
		ReviewedBy: 10, ReviewedByName: "reviewer",
	}
	claimed, err := reviewRepo.UpsertDecision(context.Background(), &first)
	if err != nil || !claimed {
		t.Fatalf("first claim claimed=%v err=%v", claimed, err)
	}
	// A different reviewer must not overwrite the first claim.
	second := model.PhaseReview{
		AnalysisID: 1, Phase: "lag", Decision: "excluded", Cause: "c", Rationale: "r",
		ReviewedBy: 11, ReviewedByName: "scientist",
	}
	claimed, err = reviewRepo.UpsertDecision(context.Background(), &second)
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("second reviewer must not take over a phase claimed by someone else")
	}
	reviews, err := reviewRepo.ListByAnalysis(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].ReviewedBy != 10 || reviews[0].Decision != "accepted" {
		t.Fatalf("claim was overwritten: %+v", reviews)
	}
	// The owner can revise their own decision.
	revision := model.PhaseReview{
		AnalysisID: 1, Phase: "lag", Decision: "accepted", Rationale: "rechecked",
		ReviewedBy: 10, ReviewedByName: "reviewer",
	}
	claimed, err = reviewRepo.UpsertDecision(context.Background(), &revision)
	if err != nil || !claimed {
		t.Fatalf("owner revision claimed=%v err=%v", claimed, err)
	}
	reviews, _ = reviewRepo.ListByAnalysis(context.Background(), 1)
	if reviews[0].Rationale != "rechecked" {
		t.Fatalf("owner revision not stored: %+v", reviews[0])
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func firstPhase(phases []string, excluded string) string {
	for _, phase := range phases {
		if phase != excluded {
			return phase
		}
	}
	return ""
}
