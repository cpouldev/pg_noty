package config

func acceptanceSpecsR43() []acceptanceSpec {
	return []acceptanceSpec{
		wholeAcceptance(R43, "R43_ok_is_distinct_boolean",
			[]acceptanceSubject{
				scalarSubject("is_distinct.true",
					"$.listeners[0].operations.update.is_distinct", "true", "false"),
				scalarSubject("is_distinct.false",
					"$.listeners[1].operations.update.is_distinct", "false", "true"),
			},
			[]acceptanceObservation{
				configObservation("is_distinct.true", "the true is_distinct state",
					func(c Config) bool {
						op, exists := listenerOperation(c, 0, updateOperation)
						return exists && op.IsDistinct
					}),
				configObservation("is_distinct.false", "the false is_distinct state",
					func(c Config) bool {
						op, exists := listenerOperation(c, 1, updateOperation)
						return exists && !op.IsDistinct
					}),
			}),
	}
}
