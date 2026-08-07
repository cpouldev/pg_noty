package config

import (
	"slices"
	"strings"
)

func acceptanceSpecsR22ToR29() []acceptanceSpec {
	return []acceptanceSpec{
		wholeAcceptance(R22, "R22_ok_listeners_empty_list",
			[]acceptanceSubject{
				sequenceSubject("listeners.empty", "$.listeners", nil,
					replaceText("listeners: []\n", oneAcceptanceListener)),
			},
			[]acceptanceObservation{
				configObservation("listeners.empty", "the explicit empty listener list",
					func(c Config) bool { return len(c.Listeners) == 0 }),
			}),
		halfAcceptance(R23, r23PresenceHalf, "R23_ok_listener_name_present",
			[]acceptanceSubject{
				mappingKeySubject("listener.name.presence", "$.listeners[0]", "name"),
			},
			[]acceptanceObservation{
				positionedKeyObservation("listener.name.presence", "$.listeners[0]", "name"),
			}),
		halfAcceptance(R23, r23ValueHalf, "R23_ok_listener_name_charset",
			[]acceptanceSubject{
				scalarSubject("listener.name.max", "$.listeners[0].name",
					strings.Repeat("a", 41), strings.Repeat("b", 40)),
			},
			[]acceptanceObservation{
				configObservation("listener.name.max", "the 41-rune listener-name boundary",
					func(c Config) bool {
						return len(c.Listeners) == 1 &&
							c.Listeners[0].Name == strings.Repeat("a", 41)
					}),
			}),
		uniqueListenerNamesAcceptance(),
		listenerEnabledAcceptance(),
		halfAcceptance(R26, r26PresenceHalf, "R26_ok_listener_table_present",
			[]acceptanceSubject{
				mappingKeySubject("listener.table.presence", "$.listeners[0]", "table"),
			},
			[]acceptanceObservation{
				positionedKeyObservation("listener.table.presence", "$.listeners[0]", "table"),
			}),
		halfAcceptance(R26, r26ValueHalf, "R26_ok_table_part_63_bytes",
			[]acceptanceSubject{
				scalarSubject("listener.table.max", "$.listeners[0].table",
					"public."+strings.Repeat("a", 63), "public."+strings.Repeat("b", 62)),
			},
			[]acceptanceObservation{
				configObservation("listener.table.max", "the 63-byte table-part boundary",
					func(c Config) bool {
						return len(c.Listeners) == 1 &&
							c.Listeners[0].Trigger.Table == "public."+strings.Repeat("a", 63)
					}),
			}),
		operationFormsAcceptance(),
		allOperationsAcceptance(),
		halfAcceptance(R29, r29LegalityHalf, "R29_ok_columns_under_update",
			[]acceptanceSubject{
				sequenceSubject("update.columns", "$.listeners[0].operations.update.columns",
					[]string{"status"},
					mutateScalar("$.listeners[0].operations.update.columns[0]", "state")),
			},
			[]acceptanceObservation{
				configObservation("update.columns", "columns under update",
					func(c Config) bool {
						op, ok := listenerOperation(c, 0, "update")
						return ok && slices.Equal(op.Columns, []string{"status"})
					}),
			}),
		updateColumnContentAcceptance(),
	}
}

const oneAcceptanceListener = `listeners:
  - name: one
    table: public.orders
    operations: [insert]
    destination:
      url: https://example.test/one
`

func uniqueListenerNamesAcceptance() acceptanceSpec {
	return wholeAcceptance(R24, "R24_ok_unique_listener_names",
		[]acceptanceSubject{
			scalarSubject("listener.name.order_paid",
				"$.listeners[0].name", "order_paid", "order_created"),
			scalarSubject("listener.name.order_archived",
				"$.listeners[1].name", "order_archived", "order_deleted"),
		},
		[]acceptanceObservation{
			configObservation("listener.name.order_paid", "the order_paid identity",
				func(c Config) bool { return len(c.Listeners) == 2 && c.Listeners[0].Name == "order_paid" }),
			configObservation("listener.name.order_archived", "the distinct order_archived identity",
				func(c Config) bool {
					return len(c.Listeners) == 2 && c.Listeners[1].Name == "order_archived"
				}),
		})
}

func listenerEnabledAcceptance() acceptanceSpec {
	return wholeAcceptance(R25, "R25_ok_listener_enabled_boolean",
		[]acceptanceSubject{
			scalarSubject("listener.enabled.true", "$.listeners[0].enabled", "true", "false"),
			scalarSubject("listener.enabled.false", "$.listeners[1].enabled", "false", "true"),
		},
		[]acceptanceObservation{
			configObservation("listener.enabled.true", "the true enabled state",
				func(c Config) bool { return len(c.Listeners) == 2 && c.Listeners[0].Enabled }),
			configObservation("listener.enabled.false", "the false enabled state",
				func(c Config) bool { return len(c.Listeners) == 2 && !c.Listeners[1].Enabled }),
		})
}

func operationFormsAcceptance() acceptanceSpec {
	return wholeAcceptance(R27, "R27_ok_operations_one_entry",
		[]acceptanceSubject{
			scalarSubject("operations.list.insert",
				"$.listeners[0].operations[0]", "insert", "delete"),
			mappingKeySubjectUsing("operations.map.update", "$.listeners[1].operations", "update",
				replaceMappingKey("$.listeners[1].operations", "update", "delete")),
		},
		[]acceptanceObservation{
			configObservation("operations.list.insert", "the list-form insert operation",
				func(c Config) bool {
					return len(c.Listeners) == 2 &&
						operationKindsEqual(c.Listeners[0], []string{"insert"})
				}),
			configObservation("operations.map.update", "the map-form update operation",
				func(c Config) bool {
					return len(c.Listeners) == 2 &&
						operationKindsEqual(c.Listeners[1], []string{"update"})
				}),
		})
}

func allOperationsAcceptance() acceptanceSpec {
	return wholeAcceptance(R28, "R28_ok_all_three_operations",
		[]acceptanceSubject{
			mappingKeySubject("operations.insert", "$.listeners[0].operations", "insert"),
			mappingKeySubject("operations.update", "$.listeners[0].operations", "update"),
			mappingKeySubject("operations.delete", "$.listeners[0].operations", "delete"),
		},
		[]acceptanceObservation{
			operationKindObservation("operations.insert", "insert"),
			operationKindObservation("operations.update", "update"),
			operationKindObservation("operations.delete", "delete"),
		})
}

func operationKindObservation(subject, kind string) acceptanceObservation {
	return configObservation(subject, "the "+kind+" operation", func(c Config) bool {
		_, exists := listenerOperation(c, 0, kind)
		return exists
	})
}

func updateColumnContentAcceptance() acceptanceSpec {
	const base = "$.listeners[0].operations.update.columns"
	return halfAcceptance(R29, r29ContentHalf, "R29_ok_update_column_content",
		[]acceptanceSubject{
			scalarSubject("columns.id", base+"[0]", "id", "other_id"),
			scalarSubject("columns.Total", base+"[1]", "Total", "Amount"),
			scalarSubject("columns.Δelta", base+"[2]", "Δelta", "Σigma"),
		},
		[]acceptanceObservation{
			updateColumnObservation("columns.id", 0, "id"),
			updateColumnObservation("columns.Total", 1, "Total"),
			updateColumnObservation("columns.Δelta", 2, "Δelta"),
		})
}

func updateColumnObservation(subject string, index int, want string) acceptanceObservation {
	return configObservation(subject, "update column "+want, func(c Config) bool {
		op, exists := listenerOperation(c, 0, "update")
		return exists && len(op.Columns) == 3 && op.Columns[index] == want
	})
}
