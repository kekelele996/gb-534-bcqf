package algorithm

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"fermentation-kinetics-deviation-analysis/backend/internal/constants"
	"fermentation-kinetics-deviation-analysis/backend/internal/model"
	"fermentation-kinetics-deviation-analysis/backend/internal/timeseries"
	"fermentation-kinetics-deviation-analysis/backend/internal/util"
)

func TestDTWDeterministicFixture(t *testing.T) {
	distance, path, err := DTW([]float64{1, 2, 3, 4}, []float64{1, 2, 3, 4}, 2)
	if err != nil {
		t.Fatalf("DTW: %v", err)
	}
	if distance != 0 || len(path) != 4 {
		t.Fatalf("distance=%v path=%v", distance, path)
	}
}

func TestEvaluatorIsDeterministicAndReplayable(t *testing.T) {
	snapshot := evaluatorFixture(t)
	first, err := NewEvaluator().Evaluate(snapshot)
	if err != nil {
		t.Fatalf("first evaluate: %v", err)
	}
	second, err := NewEvaluator().Evaluate(snapshot)
	if err != nil {
		t.Fatalf("second evaluate: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("results differ:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if first.DeviationLevel != constants.DeviationNormal {
		t.Fatalf("identical fixture level=%s, want normal", first.DeviationLevel)
	}
	encoded, err := snapshot.Canonical()
	if err != nil {
		t.Fatalf("canonical snapshot: %v", err)
	}
	decoded, err := DecodeSnapshot(encoded)
	if err != nil || !reflect.DeepEqual(snapshot, decoded) {
		t.Fatalf("snapshot replay mismatch: %v", err)
	}
}

func TestVersionedSnapshotsRemainReplayable(t *testing.T) {
	snapshot := evaluatorFixture(t)
	// Current version embeds per-phase suspected causes on phases with rule
	// hits; phases without hits omit the field (identical JSON to v1 for
	// replay fidelity).
	current, err := NewEvaluator().Evaluate(snapshot)
	if err != nil {
		t.Fatalf("current evaluate: %v", err)
	}
	var currentEvidence []PhaseEvidence
	if err := json.Unmarshal([]byte(current.PhaseScoresJSON), &currentEvidence); err != nil {
		t.Fatal(err)
	}
	if len(currentEvidence) != 4 {
		t.Fatalf("expected four phase evidence entries, got %d", len(currentEvidence))
	}
	// A frozen v1.0.0 snapshot stays decodable and produces legacy output
	// without the per-phase cause field, so historical results remain
	// byte-for-byte reproducible.
	legacy := snapshot
	legacy.AlgorithmVersion = PhaseConstraintVersionV1
	if !SupportsVersion(PhaseConstraintVersionV1) {
		t.Fatal("v1.0.0 snapshots must remain supported for replay")
	}
	legacyResult, err := NewEvaluator().Evaluate(legacy)
	if err != nil {
		t.Fatalf("legacy evaluate: %v", err)
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(legacyResult.PhaseScoresJSON), &raw); err != nil {
		t.Fatal(err)
	}
	for _, evidence := range raw {
		if _, present := evidence["suspected_causes"]; present {
			t.Fatalf("v1.0.0 replay must not emit the per-phase suspected_causes field: %v", evidence)
		}
	}
	candidates, err := PhaseCandidateCauses(legacy)
	if err != nil {
		t.Fatalf("candidate causes: %v", err)
	}
	if len(candidates) != 4 {
		t.Fatalf("expected candidates for four phases, got %d", len(candidates))
	}
	// A high-deviation run under v1.1 embeds per-phase rule hits, while the
	// same frozen snapshot under v1.0.0 omits the field for replay fidelity.
	high := evaluatorDeviationFixture(t)
	highCurrent, err := NewEvaluator().Evaluate(high)
	if err != nil {
		t.Fatalf("high deviation evaluate: %v", err)
	}
	var highEvidence []map[string]any
	if err := json.Unmarshal([]byte(highCurrent.PhaseScoresJSON), &highEvidence); err != nil {
		t.Fatal(err)
	}
	embedded := false
	for _, evidence := range highEvidence {
		if _, ok := evidence["suspected_causes"]; ok {
			embedded = true
		}
	}
	if !embedded {
		t.Fatalf("%s high-deviation result must embed per-phase suspected causes", Version)
	}
	high.AlgorithmVersion = PhaseConstraintVersionV1
	highLegacy, err := NewEvaluator().Evaluate(high)
	if err != nil {
		t.Fatalf("high deviation legacy evaluate: %v", err)
	}
	var legacyHighEvidence []map[string]any
	if err := json.Unmarshal([]byte(highLegacy.PhaseScoresJSON), &legacyHighEvidence); err != nil {
		t.Fatal(err)
	}
	for _, evidence := range legacyHighEvidence {
		if _, ok := evidence["suspected_causes"]; ok {
			t.Fatalf("v1.0.0 replay must not embed per-phase suspected causes: %v", evidence)
		}
	}
}

func evaluatorDeviationFixture(t *testing.T) Snapshot {
	t.Helper()
	started := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	points := make([]timeseries.Point, 0, 9)
	reference := map[string][]CurvePoint{"ph": {}}
	for hour := 0; hour <= 8; hour++ {
		referenceValue := 7.0 - float64(hour)*0.05
		actualValue := referenceValue - 1.2*float64(hour%2)
		points = append(points, timeseries.Point{
			Timestamp: started.Add(time.Duration(hour) * time.Hour),
			Values:    map[string]*float64{"ph": &actualValue},
		})
		reference["ph"] = append(reference["ph"], CurvePoint{ElapsedHour: float64(hour), Value: referenceValue})
	}
	pointsJSON, err := timeseries.EncodePoints(points)
	if err != nil {
		t.Fatal(err)
	}
	boundaries, _ := util.CanonicalJSON([]PhaseBoundary{
		{Phase: constants.PhaseLag, StartHour: 0, EndHour: 2},
		{Phase: constants.PhaseGrowth, StartHour: 2, EndHour: 4},
		{Phase: constants.PhaseProduction, StartHour: 4, EndHour: 6},
		{Phase: constants.PhaseHarvest, StartHour: 6, EndHour: 8},
	})
	references, _ := util.CanonicalJSON(reference)
	tolerances, _ := util.CanonicalJSON(map[string]ChannelTolerance{"ph": {Weight: 1, MaxDistance: 0.3}})
	series := model.SensorSeries{
		ID: 3, VesselID: 2, RecipeID: 4, RunCode: "FIXTURE-HIGH", Channel: "ph",
		SampleIntervalS: 3600, PointsJSON: pointsJSON, SourceChecksum: util.HashString(pointsJSON),
		StartedAt: started, EndedAt: started.Add(8 * time.Hour),
	}
	recipe := model.CultureRecipe{
		ID: 4, Version: 2, TargetDurationH: 8, PhaseBoundariesJSON: boundaries,
		ReferenceCurvesJSON: references, ToleranceProfileJSON: tolerances,
	}
	return NewSnapshot(series, recipe)
}

func evaluatorFixture(t *testing.T) Snapshot {
	t.Helper()
	started := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	points := make([]timeseries.Point, 0, 9)
	reference := map[string][]CurvePoint{"ph": {}}
	for hour := 0; hour <= 8; hour++ {
		value := 7.0 - float64(hour)*0.05
		valueCopy := value
		points = append(points, timeseries.Point{
			Timestamp: started.Add(time.Duration(hour) * time.Hour),
			Values:    map[string]*float64{"ph": &valueCopy},
		})
		reference["ph"] = append(reference["ph"], CurvePoint{ElapsedHour: float64(hour), Value: value})
	}
	pointsJSON, err := timeseries.EncodePoints(points)
	if err != nil {
		t.Fatal(err)
	}
	boundaries, _ := util.CanonicalJSON([]PhaseBoundary{
		{Phase: constants.PhaseLag, StartHour: 0, EndHour: 2},
		{Phase: constants.PhaseGrowth, StartHour: 2, EndHour: 4},
		{Phase: constants.PhaseProduction, StartHour: 4, EndHour: 6},
		{Phase: constants.PhaseHarvest, StartHour: 6, EndHour: 8},
	})
	references, _ := util.CanonicalJSON(reference)
	tolerances, _ := util.CanonicalJSON(map[string]ChannelTolerance{"ph": {Weight: 1, MaxDistance: 1}})
	series := model.SensorSeries{
		ID: 3, VesselID: 2, RecipeID: 4, RunCode: "FIXTURE", Channel: "ph",
		SampleIntervalS: 3600, PointsJSON: pointsJSON, SourceChecksum: util.HashString(pointsJSON),
		StartedAt: started, EndedAt: started.Add(8 * time.Hour),
	}
	recipe := model.CultureRecipe{
		ID: 4, Version: 2, TargetDurationH: 8, PhaseBoundariesJSON: boundaries,
		ReferenceCurvesJSON: references, ToleranceProfileJSON: tolerances,
	}
	return NewSnapshot(series, recipe)
}
