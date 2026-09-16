package astroclient_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/astroclient"
)

// The generated client, checked against the committed contract.
//
// `task contracts` regenerates the OpenAPI document from the Pydantic
// models and the Go client from that document. CI fails if the committed
// output differs from what the source produces, which is what makes a
// contract change visible in review rather than a surprise at runtime.
//
// What CI cannot check by diffing is whether the SHAPE still carries the
// properties the system depends on. That is what this file is for.

func loadContract(t *testing.T) map[string]any {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "..", "..", "contracts", "openapi", "astro.json"))
	if err != nil {
		t.Fatalf("resolve contract path: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		// Fatal, never skip. A missing contract would otherwise make this
		// whole file pass while asserting nothing — the failure mode this
		// repo has already hit twice.
		t.Fatalf("read %s: %v", path, err)
	}

	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("parse contract: %v", err)
	}
	return document
}

func TestTheContractDescribesEveryEndpointTheGoSideNeeds(t *testing.T) {
	paths, ok := loadContract(t)["paths"].(map[string]any)
	if !ok {
		t.Fatal("the contract has no paths")
	}

	for _, endpoint := range []string{
		"/v1/charts/compute",
		"/v1/dashas/compute",
		"/v1/transits/compute",
		"/health",
	} {
		if _, present := paths[endpoint]; !present {
			t.Errorf("%s is absent from the contract; api-service cannot call it", endpoint)
		}
	}
}

// The property the whole unknown-birth-time contract rests on, at the
// language boundary.
//
// A large fraction of Indian users genuinely do not know their birth
// time. astro-service answers honestly by omitting the ascendant rather
// than inventing one — but that honesty only survives into Go if the
// generated field is a POINTER.
//
// If it were a value type, "no ascendant" would arrive as a zero-valued
// AscendantPosition: sign "", sign_index 0, degree 0. Sign index 0 is
// Aries. The Go service would then persist, and the UI would display, a
// confident 0° Aries rising for someone whose rising sign is unknowable.
func TestAbsentFieldsArrivePointersSoNilSurvives(t *testing.T) {
	var chart astroclient.ChartResponse

	if chart.Ascendant != nil {
		t.Fatal("a zero-valued ChartResponse has a non-nil ascendant")
	}
	if chart.Houses != nil {
		t.Error("houses is not nillable")
	}
	if chart.Dashas != nil {
		t.Error("dashas is not nillable")
	}
	if chart.Navamsa != nil {
		t.Error("navamsa is not nillable")
	}
}

// Decoding the real "unknown birth time" payload.
//
// The type-level check above proves the fields CAN be nil. This proves
// they actually are, for the JSON astro-service sends.
func TestAnUnknownTimeChartDecodesWithNilAscendant(t *testing.T) {
	// Trimmed to the fields under test; the rest of the document is
	// covered by the Python side's own tests.
	payload := `{
		"meta": {
			"schema_version": 1, "calculation_system": "vedic",
			"ayanamsa": "lahiri", "ayanamsa_value": 23.8,
			"house_system": "whole_sign", "engine_version": "skyfield-1.55+de421+schema1",
			"computed_at": "2026-01-01T00:00:00Z", "time_accuracy": "unknown"
		},
		"ascendant": null, "houses": null, "dashas": null, "navamsa": null,
		"planets": [], "yogas": [],
		"summary": {
			"sun_sign": "Leo", "moon_sign": "Sagittarius", "ascendant_sign": null,
			"moon_nakshatra": "Mula", "moon_nakshatra_pada": 4
		}
	}`

	var chart astroclient.ChartResponse
	if err := json.Unmarshal([]byte(payload), &chart); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if chart.Ascendant != nil {
		t.Error("an explicit null ascendant decoded to something non-nil")
	}
	if chart.Summary.AscendantSign != nil {
		t.Error("a null ascendant_sign in the summary decoded to something non-nil")
	}

	// The facts that survive a missing birth time must still be there.
	if chart.Summary.MoonSign != "Sagittarius" {
		t.Errorf("moon_sign = %q; it survives an unknown birth time", chart.Summary.MoonSign)
	}
}

// engine_version identifies exactly what produced a chart, so a library
// upgrade can be followed by a targeted recompute. The schema version has
// to be part of it: otherwise charts written under two different shapes
// are indistinguishable in the database and the "find charts built by an
// older engine" query silently misses them.
func TestTheContractPinsTheSchemaVersion(t *testing.T) {
	document := loadContract(t)

	components, _ := document["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	meta, ok := schemas["ChartMeta"].(map[string]any)
	if !ok {
		t.Fatal("ChartMeta is missing from the contract")
	}

	properties, _ := meta["properties"].(map[string]any)
	for _, field := range []string{"schema_version", "engine_version", "ayanamsa_value"} {
		if _, present := properties[field]; !present {
			t.Errorf("ChartMeta.%s is absent; provenance would be unrecoverable", field)
		}
	}
}

// The request side forbids unknown fields, and that has to reach the
// contract or the Go client will happily send a typo'd key and get a
// chart for a different birth time than it asked for.
func TestTheRequestRefusesUnknownFields(t *testing.T) {
	document := loadContract(t)

	components, _ := document["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	birth, ok := schemas["BirthData"].(map[string]any)
	if !ok {
		t.Fatal("BirthData is missing from the contract")
	}

	additional, present := birth["additionalProperties"]
	if !present {
		t.Fatal("BirthData does not declare additionalProperties; extra=\"forbid\" was lost")
	}
	if allowed, isBool := additional.(bool); !isBool || allowed {
		t.Errorf("BirthData allows additional properties (%v); a typo'd field would be ignored", additional)
	}
}

// Coordinates are bounded in the contract, not just in Python.
//
// A Go caller building a request from bad data should be catchable
// before the HTTP round trip, and a generated client can only offer that
// if the bounds are in the document.
func TestCoordinateBoundsSurviveIntoTheContract(t *testing.T) {
	document := loadContract(t)

	components, _ := document["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	birth, _ := schemas["BirthData"].(map[string]any)
	properties, _ := birth["properties"].(map[string]any)

	for field, bounds := range map[string][2]float64{
		"latitude":  {-90, 90},
		"longitude": {-180, 180},
	} {
		spec, ok := properties[field].(map[string]any)
		if !ok {
			t.Errorf("%s is absent from BirthData", field)
			continue
		}
		minimum, hasMin := spec["minimum"].(float64)
		maximum, hasMax := spec["maximum"].(float64)

		if !hasMin || !hasMax {
			t.Errorf("%s has no bounds in the contract", field)
			continue
		}
		if minimum != bounds[0] || maximum != bounds[1] {
			t.Errorf("%s bounds are [%v, %v], expected %v", field, minimum, maximum, bounds)
		}
	}
}

// The generated client must not have been hand-edited.
//
// The constitution says never hand-write a cross-service client. A file
// that lost its generation header is a file somebody has started editing,
// and the next `task contracts` will silently discard their work.
func TestTheClientIsGenerated(t *testing.T) {
	path := filepath.Join(".", "client.gen.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	head := string(body[:min(len(body), 400)])
	if !strings.Contains(head, "Code generated") {
		t.Error("client.gen.go has lost its generated-code header; has it been hand-edited?")
	}
}
