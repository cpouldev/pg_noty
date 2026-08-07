package config

import (
	"errors"
	"io/fs"
)

// Load reads the configuration file at path and validates it.
//
// The returned configuration is nil whenever there are diagnostics, so a caller can
// never act on a partially populated value. Warnings may be non-empty alongside a
// configuration that loaded successfully, and are returned together with any
// diagnostics from the same run rather than being withheld until errors are fixed.
//
// Both diagnostic collections are de-duplicated and ordered, because every return here
// leaves through normalizedResult rather than normalizing at each site.
func Load(path string, env EnvLookup) (*Config, Warnings, Errors) {
	// Stage A -- read. Fatal-single: a file that cannot be read has no source to
	// position against, so the one diagnostic it produces carries no position.
	src, err := readSource(path)
	if err != nil {
		return normalizedResult(nil, nil, Errors{NewFileError(RuleRead, path, readFailureMessage(err))})
	}

	return normalizedResult(runPipeline(src, env))
}

// Parse validates configuration text that has already been read. filename is used
// only to label diagnostics.
//
// This is the testable core of the package: every fixture is bytes plus a display
// name, so no test needs the filesystem. Load is stage A followed by this.
func Parse(data []byte, filename string, env EnvLookup) (*Config, Warnings, Errors) {
	return normalizedResult(runPipeline(newSource(filename, data), env))
}

// runPipeline is the stage machine. Each stage has its own error policy, and that
// policy is the contract: read, parse and document are fatal-single, interpolation
// and normalization each accumulate and then stop, and the remaining stages
// accumulate so that one run reports every mistake it can reach.
//
// What it returns is raw: de-duplication and ordering belong to the entry points that
// call it, so a stage added below cannot be the place that forgets them.
func runPipeline(src *source, env EnvLookup) (*Config, Warnings, Errors) {
	// Stages B and C -- parse and document: fatal-single. Without one bare
	// mapping root, later stages have no reliable structure to inspect.
	root, diags := parseDocument(src)
	if len(diags) != 0 {
		return nil, nil, diags
	}

	// Stage D -- interpolate: accumulate, then stop. Step 11's W1 is the first consumer of
	// provenance; retaining the value here preserves that handoff without reading its secret text.
	originals, diags := interpolate(src, root, env)
	if len(diags) != 0 {
		return nil, nil, diags
	}

	// Stage E -- normalize: accumulate, then stop. It follows interpolation so
	// a pointer-shared anchor is interpolated once before aliases expand it.
	if diags := normalize(src, root); len(diags) != 0 {
		return nil, nil, diags
	}

	// Stage F -- shape: accumulate, and stop only on a shape the stages after it cannot read.
	// Unknown or missing keys leave the tree readable, so their diagnostics continue
	// into stage G; a wrong container shape does not.
	shape, decodable := checkShape(src, root)
	if !decodable {
		return nil, nil, shape
	}

	// Stage G -- decode: wrappers record conversion diagnostics and always return nil.
	decoded, conversions := decodeDocument(src, root)

	// Stage H -- validate: accumulate every independent scalar constraint. The uniform Valid gate in
	// validate leaves absent and conversion-refused values to the stage that owns them.
	semantics := validate(src, decoded, shape)

	// Stage I -- resolve, compare effective values and compute the closed warning set. Comparisons
	// consume the merged value but retain their raw operands solely for D2's source anchor.
	resolved := resolveConfig(decoded)
	effective := validateEffective(src, decoded, resolved)
	warnings := warningsFor(decoded, resolved, originals)
	findings := append(append(append(shape, conversions...), semantics...), effective...)
	if len(findings) != 0 {
		return nil, warnings, findings
	}
	return resolved, warnings, nil
}

// readFailureMessage states why the file could not be read, without repeating the
// path that the diagnostic already carries.
func readFailureMessage(err error) string {
	reason := err

	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		reason = pathErr.Err
	}

	return "cannot read configuration file: " + reason.Error()
}
