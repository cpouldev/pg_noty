package config

func headerMergeAcceptance() acceptanceSpec {
	return wholeAcceptance(R19, "R19_ok_header_names_and_values",
		[]acceptanceSubject{
			scalarSubjectUsing("defaults.header", "$.defaults.headers.X-Default", "default",
				replaceText("defaults:\n  headers:\n    X-Default: default\n", "")),
			scalarSubjectUsing("listener.header",
				"$.listeners[0].destination.headers.User-Agent", "pg_noty",
				replaceText("      headers:\n        User-Agent: pg_noty\n", "")),
		},
		[]acceptanceObservation{
			configObservation("defaults.header", "the merged default header",
				func(c Config) bool {
					return c.Listeners[0].Delivery.Destination.Headers["X-Default"] == "default"
				}),
			configObservation("listener.header", "the listener User-Agent",
				func(c Config) bool {
					return c.Listeners[0].Delivery.Destination.Headers["User-Agent"] == "pg_noty"
				}),
		})
}

func headerIdentityAcceptance() acceptanceSpec {
	return wholeAcceptance(R20, "R20_ok_header_names_differing_beyond_case",
		[]acceptanceSubject{
			removableScalarSubject("header.trace.id", "$.defaults.headers.X-Trace-Id", "a"),
			removableScalarSubject("header.trace.ids", "$.defaults.headers.X-Trace-Ids", "b"),
		},
		[]acceptanceObservation{
			configObservation("header.trace.id", "the X-Trace-Id header",
				func(c Config) bool {
					return c.Listeners[0].Delivery.Destination.Headers["X-Trace-Id"] == "a"
				}),
			configObservation("header.trace.ids", "the distinct X-Trace-Ids header",
				func(c Config) bool {
					return c.Listeners[0].Delivery.Destination.Headers["X-Trace-Ids"] == "b"
				}),
		})
}

func nonReservedHeaderAcceptance() acceptanceSpec {
	return wholeAcceptance(R21, "R21_ok_non_reserved_header",
		[]acceptanceSubject{
			scalarSubjectUsing("header.x-pg-notice",
				"$.listeners[0].destination.headers.X-Pg-Notice", "allowed",
				replaceText("      headers:\n        X-Pg-Notice: allowed\n", "")),
		},
		[]acceptanceObservation{
			configObservation("header.x-pg-notice", "the non-reserved X-Pg header",
				func(c Config) bool {
					return c.Listeners[0].Delivery.Destination.Headers["X-Pg-Notice"] == "allowed"
				}),
		})
}
