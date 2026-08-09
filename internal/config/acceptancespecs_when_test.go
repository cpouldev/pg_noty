package config

type whenAcceptanceExtent struct {
	id          string
	path        string
	want        string
	replacement string
	mutation    acceptanceMutation
}

func acceptanceSpecR30() []acceptanceSpec {
	extents := whenAcceptanceExtents()
	subjects := make([]acceptanceSubject, 0, len(extents))
	observations := make([]acceptanceObservation, 0, len(extents))
	for listener, extent := range extents {
		mutation := extent.mutation
		if mutation == nil {
			mutation = mutateScalar(extent.path, extent.replacement)
		}
		subjects = append(subjects,
			scalarSubjectUsing(extent.id, extent.path, extent.want, mutation))
		listener, extent := listener, extent
		observations = append(observations,
			configObservation(extent.id, "the exact accepted WHEN "+extent.want,
				func(c Config) bool { return listenerWhen(c, listener) == extent.want }))
	}
	return []acceptanceSpec{
		wholeAcceptance(R30, "R30_ok_adversarial_when_clauses", subjects, observations),
	}
}

func listenerWhen(cfg Config, listener int) string {
	if listener < 0 || listener >= len(cfg.Listeners) ||
		len(cfg.Listeners[listener].Trigger.Operations) != 1 {
		return ""
	}
	return cfg.Listeners[listener].Trigger.Operations[0].When
}

func whenAcceptanceExtents() []whenAcceptanceExtent {
	return []whenAcceptanceExtent{
		{"when.single-quote", "$.listeners[0].operations.insert.when",
			"status = 'OLD.status'", `"NEW.status = 1"`, nil},
		{"when.dollar-quote", "$.listeners[1].operations.insert.when",
			"$tag$OLD.status$tag$", `"NEW.status > 2"`, nil},
		{"when.line-comment", "$.listeners[2].operations.insert.when",
			"-- OLD.status\nNEW.status > 0", "", replaceText(
				"-- OLD.status\n          NEW.status > 0",
				"-- changed\n          NEW.status > 0")},
		{"when.block-comment", "$.listeners[3].operations.insert.when",
			"/* OLD.status */ NEW.status > 0", `"/* changed */ NEW.status > 0"`, nil},
		{"when.quoted-identifier", "$.listeners[4].operations.insert.when",
			`"OLD".status = 1`, `"NEW.status = 1"`, nil},
		{"when.long-word", "$.listeners[5].operations.insert.when",
			"old_price > 0", `"new_price > 0"`, nil},
		{"when.max-tag", "$.listeners[6].operations.insert.when",
			"$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa$OLD.status$" +
				"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa$",
			`"$tag$OLD.status$tag$"`, nil},
		{"when.non-ascii-tag", "$.listeners[7].operations.insert.when",
			"$·$OLD.status$·$", `"$x$OLD.status$x$"`, nil},
		{"when.non-ascii-word", "$.listeners[8].operations.insert.when",
			"€old > 0", `"€older > 0"`, nil},
		{"when.escape-string", "$.listeners[9].operations.insert.when",
			"E'continued\\\n-- OLD.status\nstill string'", "", replaceText(
				"-- OLD.status\n          still string'",
				"-- changed\n          still string'")},
		{"when.old-field", "$.listeners[10].operations.insert.when",
			"NEW.old > 1", `"NEW.older > 1"`, nil},
		{"when.new-field", "$.listeners[11].operations.delete.when",
			"OLD.new > 1", `"OLD.newer > 1"`, nil},
	}
}
