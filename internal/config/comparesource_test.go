package config

import (
	"testing"
)

func stageIComparisonCandidates(t *testing.T, data []byte) Errors {
	t.Helper()
	src := newSource("inherited.yaml", data)
	root, diags := parseDocument(src)
	if len(diags) != 0 {
		t.Fatalf("parse failed: %+v", diags)
	}
	if _, diags = interpolate(src, root, MapEnv(nil)); len(diags) != 0 {
		t.Fatalf("interpolation failed: %+v", diags)
	}
	if diags = normalize(src, root); len(diags) != 0 {
		t.Fatalf("normalization failed: %+v", diags)
	}
	shape, readable := checkShape(src, root)
	raw, conversions := decodeDocument(src, root)
	if !readable || len(shape)+len(conversions) != 0 {
		t.Fatalf("fixture is not stage-I readable: shape=%+v conversions=%+v", shape, conversions)
	}
	return validateEffective(src, raw, resolveConfig(raw))
}
