package config

import (
	"strconv"
	"strings"
	"testing"
)

func TestPayloadCompatibilityDecisionTable(t *testing.T) {
	tests := []struct {
		name, mode, operands string
		rule                 RuleID
		path, anchor         string
		want                 int
	}{
		{"full neither", "full", "", noRule, "", "", 0},
		{"columns present", "columns", "      columns: [id]\n", noRule, "", "", 0},
		{"keys only neither", "keys_only", "", noRule, "", "", 0},
		{"columns missing", "columns", "", R32, "listeners[0].payload.mode", "columns", 1},
		{"full with columns", "full", "      columns: [id]\n", R32,
			"listeners[0].payload.columns", "[id]", 1},
		{"keys only with exclude", "keys_only", "      exclude: [internal_note]\n", R33,
			"listeners[0].payload.exclude", "[internal_note]", 1},
		{"both operands", "full", "      columns: [id]\n      exclude: [internal_note]\n", R33,
			"listeners[0].payload.exclude", "[internal_note]", 1},
		{"invalid mode suppresses compatibility", "sideways", "      exclude: [internal_note]\n", R31,
			"listeners[0].payload.mode", "sideways", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			document := payloadDocument(tc.mode, tc.operands)
			_, diags := stageH(t, document)
			if len(diags) != tc.want {
				t.Fatalf("got %d diagnostics %q, want %d", len(diags), messagesOf(diags), tc.want)
			}
			if tc.want == 0 {
				return
			}
			if diags[0].Rule != tc.rule || diags[0].Path != tc.path {
				t.Errorf("diagnostic = %+v, want %s at %s", diags[0], tc.rule, tc.path)
			}
			line, column := occurrencePosition(t, document, tc.anchor, 1)
			if diags[0].Line != line || diags[0].Col != column {
				t.Errorf("diagnostic at %d:%d, want %q at %d:%d",
					diags[0].Line, diags[0].Col, tc.anchor, line, column)
			}
		})
	}
}

func TestColumnListContentDiagnosticsAnchorOnTheirElement(t *testing.T) {
	for _, extent := range []string{"payload columns", "payload exclude", "update columns"} {
		for _, defect := range []string{"duplicate", "empty", "invalid"} {
			t.Run(extent+"/"+defect, func(t *testing.T) {
				list, token, occurrence, index := listDefect(defect)
				document, rule, path := listDocument(extent, list)
				assertStructuredDiagnostic(t, document, rule, path+"["+index+"]",
					token, occurrence, listMessage(defect))
				if defect == "duplicate" {
					_, diags := stageH(t, document)
					firstLine, _ := occurrencePosition(t, document, token, 1)
					if !strings.Contains(diags[0].Msg, "line "+strconv.Itoa(firstLine)) {
						t.Errorf("duplicate message %q does not name first occurrence line %d",
							diags[0].Msg, firstLine)
					}
				}
			})
		}
	}
}

func TestSigningSecretsContentAndSizeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, list, path, token, message string
		occurrence                       int
		want                             int
	}{
		{"empty list", "[]", "", "", "", 0, 0},
		{"one entry", "[current]", "", "", "", 0, 0},
		{"empty entry", `[""]`, "listeners[0].destination.signing.secrets[0]",
			`""`, "empty", 1, 1},
		{"duplicate entry", "[same_secret, same_secret]",
			"listeners[0].destination.signing.secrets[1]", "same_secret", "line", 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document := signingDocument(tc.list)
			_, diags := stageH(t, document)
			if len(diags) != tc.want {
				t.Fatalf("got %d diagnostics %q, want %d", len(diags), messagesOf(diags), tc.want)
			}
			if tc.want == 0 {
				return
			}
			got := diags[0]
			if got.Rule != R38 || got.Path != tc.path {
				t.Errorf("diagnostic = %+v, want R38 at %s", got, tc.path)
			}
			line, column := occurrencePosition(t, document, tc.token, tc.occurrence)
			if got.Line != line || got.Col != column || !strings.Contains(got.Msg, tc.message) {
				t.Errorf("diagnostic = %+v, want %q at %d:%d containing %q",
					got, tc.token, line, column, tc.message)
			}
		})
	}
}

func payloadDocument(mode, operands string) string {
	return aListenerOf(requiredName, requiredTable, requiredOperations,
		"    payload:\n      mode: "+mode+"\n"+operands, requiredDestination)
}

func listDefect(defect string) (list, token string, occurrence int, index string) {
	switch defect {
	case "duplicate":
		return "\n          - duplicate_column\n          - duplicate_column\n",
			"duplicate_column", 2, "1"
	case "empty":
		return "\n          - \"\"\n", `""`, 1, "0"
	default:
		return "\n          - bad-column\n", "bad-column", 1, "0"
	}
}

func listDocument(extent, list string) (string, RuleID, string) {
	switch extent {
	case "payload columns":
		return payloadDocument("columns", "      columns:"+list), R32, "listeners[0].payload.columns"
	case "payload exclude":
		return payloadDocument("full", "      exclude:"+list), R33, "listeners[0].payload.exclude"
	default:
		operations := "    operations:\n      update:\n        columns:" + list
		return aListenerOf(requiredName, requiredTable, operations, requiredDestination),
			R29, "listeners[0].operations.update.columns"
	}
}

func signingDocument(list string) string {
	return destinationOf("    destination:\n" +
		"      url: https://hooks.example.test/order-paid\n" +
		"      signing:\n        secrets: " + list + "\n")
}

func listMessage(defect string) string {
	if defect == "duplicate" {
		return "line"
	}
	if defect == "empty" {
		return "empty"
	}
	return "identifier"
}
