package config

type requiredKeyAnchorCase struct {
	name     string
	document string
	wantRule RuleID
	wantKey  string
	wantLine int
	wantCol  int
}

func requiredKeyAnchorCases() []requiredKeyAnchorCase {
	return []requiredKeyAnchorCase{
		{
			name: "no version", document: "database:\n  url: postgres://noty:pw@db/noty\nlisteners: []\n",
			wantRule: R1, wantKey: "version", wantLine: rootLine, wantCol: rootColumn,
		},
		{
			name:     "no database.url",
			document: "version: 1\ndatabase:\n  schema: noty\nlisteners: []\n",
			wantRule: R3, wantKey: "database.url", wantLine: rootLine, wantCol: rootColumn,
		},
		{
			name: "no database block at all", document: "version: 1\nlisteners: []\n",
			wantRule: R3, wantKey: "database.url", wantLine: rootLine, wantCol: rootColumn,
		},
		{
			name:     "no listeners",
			document: "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\n",
			wantRule: R22, wantKey: "listeners", wantLine: rootLine, wantCol: rootColumn,
		},
		{
			name:     "a listener with no name",
			document: aListenerOf(requiredTable, requiredOperations, requiredDestination),
			wantRule: R23, wantKey: "name", wantLine: listenerLine, wantCol: listenerColumn,
		},
		{
			name:     "a listener with no table",
			document: aListenerOf(requiredName, requiredOperations, requiredDestination),
			wantRule: R26, wantKey: "table", wantLine: listenerLine, wantCol: listenerColumn,
		},
		{
			name:     "a listener with no operations",
			document: aListenerOf(requiredName, requiredTable, requiredDestination),
			wantRule: R27, wantKey: operationsKey, wantLine: listenerLine, wantCol: listenerColumn,
		},
		{
			name: "a listener with no destination.url",
			document: aListenerOf(requiredName, requiredTable, requiredOperations,
				"    destination:\n      method: POST\n"),
			wantRule: R36, wantKey: "destination.url",
			wantLine: listenerLine, wantCol: listenerColumn,
		},
		{
			name:     "a tagged database block with no url",
			document: "version: 1\ndatabase: !!map\n  schema: noty\nlisteners: []\n",
			wantRule: R3, wantKey: "database.url", wantLine: rootLine, wantCol: rootColumn,
		},
		{
			name: "a tagged destination block with no url",
			document: aListenerOf(requiredName, requiredTable, requiredOperations,
				"    destination: !!map\n      method: POST\n"),
			wantRule: R36, wantKey: "destination.url",
			wantLine: listenerLine, wantCol: listenerColumn,
		},
		{
			name:     "a listener with no destination block at all",
			document: aListenerOf(requiredName, requiredTable, requiredOperations),
			wantRule: R36, wantKey: "destination.url",
			wantLine: listenerLine, wantCol: listenerColumn,
		},
	}
}
