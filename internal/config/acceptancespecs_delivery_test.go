package config

import (
	"slices"
)

func acceptanceSpecsR31ToR38() []acceptanceSpec {
	return []acceptanceSpec{
		payloadModesAcceptance(),
		payloadColumnsAcceptance(),
		payloadExcludeAcceptance(),
		wholeAcceptance(R34, "R34_ok_include_old_boolean",
			[]acceptanceSubject{
				scalarSubject("payload.include_old", "$.listeners[0].payload.include_old",
					"true", "false"),
			},
			[]acceptanceObservation{
				configObservation("payload.include_old", "include_old true",
					func(c Config) bool {
						return len(c.Listeners) == 1 &&
							c.Listeners[0].Trigger.Payload.IncludeOld
					}),
			}),
		wholeAcceptance(R35, "R35_ok_max_bytes_positive",
			[]acceptanceSubject{
				scalarSubject("payload.max_bytes", "$.listeners[0].payload.max_bytes",
					"262144", "262143"),
			},
			[]acceptanceObservation{
				configObservation("payload.max_bytes", "max_bytes 262144",
					func(c Config) bool {
						return len(c.Listeners) == 1 &&
							c.Listeners[0].Trigger.Payload.MaxBytes == 262144
					}),
			}),
		halfAcceptance(R36, r36PresenceHalf, "R36_ok_destination_url_present",
			[]acceptanceSubject{
				mappingKeySubject("destination.url.presence",
					"$.listeners[0].destination", "url"),
			},
			[]acceptanceObservation{
				positionedKeyObservation("destination.url.presence",
					"$.listeners[0].destination", "url"),
			}),
		destinationURLValuesAcceptance(),
		destinationMethodsAcceptance(),
		signingSecretBoundariesAcceptance(),
	}
}

func payloadModesAcceptance() acceptanceSpec {
	const secondPayload = "$.listeners[1].payload"
	return wholeAcceptance(R31, "R31_ok_payload_modes",
		[]acceptanceSubject{
			scalarSubject("payload.mode.full", "$.listeners[0].payload.mode",
				"full", "keys_only"),
			scalarSubjectUsing("payload.mode.columns", secondPayload+".mode", "columns",
				combineMutations(
					mutateScalar(secondPayload+".mode", "full"),
					replaceMappingKey(secondPayload, "columns", "exclude"),
				)),
			scalarSubject("payload.mode.keys_only", "$.listeners[2].payload.mode",
				"keys_only", "full"),
		},
		[]acceptanceObservation{
			payloadModeObservation("payload.mode.full", 0, "full"),
			payloadModeObservation("payload.mode.columns", 1, "columns"),
			payloadModeObservation("payload.mode.keys_only", 2, "keys_only"),
		})
}

func payloadModeObservation(subject string, listener int, want string) acceptanceObservation {
	return configObservation(subject, "payload mode "+want, func(c Config) bool {
		return len(c.Listeners) == 3 && c.Listeners[listener].Trigger.Payload.Mode == want
	})
}

func payloadColumnsAcceptance() acceptanceSpec {
	const payload = "$.listeners[0].payload"
	return wholeAcceptance(R32, "R32_ok_payload_columns",
		[]acceptanceSubject{
			scalarSubjectUsing("payload.mode.columns", payload+".mode", "columns",
				combineMutations(
					mutateScalar(payload+".mode", "full"),
					replaceMappingKey(payload, "columns", "exclude"),
				)),
			scalarSubject("payload.columns.id", payload+".columns[0]", "id", "order_id"),
		},
		[]acceptanceObservation{
			configObservation("payload.mode.columns", "columns payload mode",
				func(c Config) bool {
					return len(c.Listeners) == 1 &&
						c.Listeners[0].Trigger.Payload.Mode == "columns"
				}),
			configObservation("payload.columns.id", "the id payload column",
				func(c Config) bool {
					return len(c.Listeners) == 1 &&
						slices.Equal(c.Listeners[0].Trigger.Payload.Columns, []string{"id"})
				}),
		})
}

func payloadExcludeAcceptance() acceptanceSpec {
	const payload = "$.listeners[0].payload"
	return wholeAcceptance(R33, "R33_ok_payload_exclude",
		[]acceptanceSubject{
			scalarSubjectUsing("payload.mode.full", payload+".mode", "full",
				combineMutations(
					mutateScalar(payload+".mode", "keys_only"),
					removeMappingKey(payload, "exclude"),
				)),
			scalarSubject("payload.exclude.internal_note",
				payload+".exclude[0]", "internal_note", "private_note"),
		},
		[]acceptanceObservation{
			configObservation("payload.mode.full", "full payload mode",
				func(c Config) bool {
					return len(c.Listeners) == 1 &&
						c.Listeners[0].Trigger.Payload.Mode == "full"
				}),
			configObservation("payload.exclude.internal_note", "the internal_note exclusion",
				func(c Config) bool {
					return len(c.Listeners) == 1 &&
						slices.Equal(c.Listeners[0].Trigger.Payload.Exclude,
							[]string{"internal_note"})
				}),
		})
}

func destinationURLValuesAcceptance() acceptanceSpec {
	return halfAcceptance(R36, r36ValueHalf, "R36_ok_http_and_https_urls",
		[]acceptanceSubject{
			scalarSubject("destination.url.http", "$.listeners[0].destination.url",
				"http://hooks.example.test/plain", "http://hooks.example.test/other"),
			scalarSubject("destination.url.https", "$.listeners[1].destination.url",
				"https://hooks.example.test/tls", "https://hooks.example.test/other"),
		},
		[]acceptanceObservation{
			destinationURLObservation("destination.url.http", 0,
				"http://hooks.example.test/plain"),
			destinationURLObservation("destination.url.https", 1,
				"https://hooks.example.test/tls"),
		})
}

func destinationURLObservation(subject string, listener int, want string) acceptanceObservation {
	return configObservation(subject, "destination URL "+want, func(c Config) bool {
		return len(c.Listeners) == 2 &&
			c.Listeners[listener].Delivery.Destination.URL == want
	})
}

func destinationMethodsAcceptance() acceptanceSpec {
	return wholeAcceptance(R37, "R37_ok_destination_methods",
		[]acceptanceSubject{
			scalarSubject("destination.method.POST",
				"$.listeners[0].destination.method", "POST", "PUT"),
			scalarSubject("destination.method.PUT",
				"$.listeners[1].destination.method", "PUT", "PATCH"),
			scalarSubject("destination.method.PATCH",
				"$.listeners[2].destination.method", "PATCH", "POST"),
		},
		[]acceptanceObservation{
			destinationMethodObservation("destination.method.POST", 0, "POST"),
			destinationMethodObservation("destination.method.PUT", 1, "PUT"),
			destinationMethodObservation("destination.method.PATCH", 2, "PATCH"),
		})
}

func destinationMethodObservation(subject string, listener int, want string) acceptanceObservation {
	return configObservation(subject, "destination method "+want, func(c Config) bool {
		return len(c.Listeners) == 3 &&
			c.Listeners[listener].Delivery.Destination.Method == want
	})
}
