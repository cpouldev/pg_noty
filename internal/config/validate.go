package config

import (
	"fmt"
	"slices"
)

type validPositioned interface {
	Positioned
	Valid() bool
}

type validatePass struct {
	stage
	prior Errors
}

// validate is stage H. It accumulates all independent scalar defects and never invents a value for
// a wrapper the decoder did not fill.
func validate(src *source, raw *rawConfig, earlier ...Errors) Errors {
	var prior Errors
	if len(earlier) != 0 {
		prior = earlier[0]
	}
	pass := &validatePass{stage: stage{src: src}, prior: prior}
	pass.validateRoot(raw)
	pass.validateDatabase(raw.Database.Value)
	pass.validateWorker(raw.Worker.Value)
	pass.validateRetention(raw.Retention.Value)
	pass.validateDefaults(raw.Defaults.Value)
	pass.validateListenerNames(raw.Listeners.Values)
	for i := range raw.Listeners.Values {
		pass.validateListener(&raw.Listeners.Values[i])
	}
	return pass.diags
}

// validateEffective is stage I's cross-field layer. D1 keeps these rules here, after resolution,
// because their operands may come from built-ins, defaults or listeners rather than one raw field.
func validateEffective(src *source, raw *rawConfig, cfg *Config) Errors {
	pass := &validatePass{stage: stage{src: src}}
	pass.validateLeaseComparison(R9, raw, cfg)
	pass.validateRetentionComparisons(R11, R12, raw, cfg)
	for i := range raw.Listeners.Values {
		pass.validateRetryComparison(R16, raw.Defaults.Value.Retry.Value,
			raw.Listeners.Values[i].Retry.Value, cfg.Listeners[i].Delivery.Retry)
		pass.validateConcurrencyComparison(R39, raw.Worker.Value.Concurrency,
			raw.Listeners.Values[i].Concurrency, cfg.Worker.Concurrency,
			cfg.Listeners[i].Delivery.Concurrency)
	}
	return pass.diags
}

func (v *validatePass) validateRoot(raw *rawConfig) {
	v.reject(R1, raw.Version, raw.Version.value != 1, semanticFault("must equal 1"))
	v.validateName(R2, raw.Instance)
}

func (v *validatePass) validateDatabase(raw rawDatabase) {
	v.validateConnection(R3, raw.URL)
	v.validateConnection(R5, raw.ListenURL)
	if !raw.Schema.Valid() {
		return
	}
	if defect := identifierDefect(raw.Schema.value); defect != identOK {
		v.reject(R4, raw.Schema, true, semanticFault(fmt.Sprintf(
			"must be a PostgreSQL identifier of at most %d bytes: %s", maxIdentifierBytes, defect,
		)))
		return
	}
	v.reject(R4, raw.Schema, usesReservedSchemaPrefix(raw.Schema.value), reservedSchemaFault)
}

func (v *validatePass) validateWorker(raw rawWorker) {
	v.validateConcurrencyRange(R6, raw.Concurrency)
	v.between(R7, raw.BatchSize, 1, 10000)
	v.positiveDuration(R8, raw.PollInterval)
	v.positiveDuration(R8, raw.LeaseTimeout)
	v.positiveDuration(R8, raw.DrainTimeout)
}

func (v *validatePass) validateRetention(raw rawRetention) {
	v.positiveDuration(R10, raw.Keep)
	v.positiveDuration(R10, raw.PartitionInterval)
	v.positiveDuration(R10, raw.Precreate)
}

func (v *validatePass) validateDefaults(raw rawDefaults) {
	v.positiveDuration(R13, raw.Timeout)
	v.validateRetry(raw.Retry.Value)
	v.validateHeaders(raw.Headers)
}

func (v *validatePass) validateRetry(raw rawRetry) {
	v.atLeast(R14, raw.MaxAttempts, 1)
	v.oneOf(R15, raw.Backoff, retryBackoffs)
	v.positiveDuration(R17, raw.MaxInterval)
}

func (v *validatePass) validateListener(raw *rawListener) {
	v.validateName(R23, raw.Name)
	v.validateTable(raw.Table)
	v.validateOperations(raw.Operations)
	v.positiveDuration(R13, raw.Timeout)
	v.validateConcurrencyRange(R39, raw.Concurrency)
	v.validateRetry(raw.Retry.Value)
	v.validatePayload(raw.Payload.Value)
	v.validateDestination(raw.Destination.Value)
}

func (v *validatePass) validatePayload(raw rawPayload) {
	v.validateIdentifierList(R32, raw.Columns, true)
	v.validateIdentifierList(R33, raw.Exclude, false)
	modeCanGovernCompatibility := payloadModeCanGovernCompatibility(raw.Mode)
	v.reject(R31, raw.Mode, !modeCanGovernCompatibility,
		semanticFault("must be one of "+readableList(payloadModes)))
	v.positiveInt(R35, raw.MaxBytes)
	if raw.Columns.Set && raw.Exclude.Set {
		v.reportStructured(R33, raw.Exclude, semanticFault(
			"payload.columns and payload.exclude are mutually exclusive"))
		return
	}
	if !modeCanGovernCompatibility {
		return
	}
	v.validatePayloadCompatibility(raw)
}

func (v *validatePass) validatePayloadCompatibility(raw rawPayload) {
	mode := raw.Mode.value
	if !raw.Mode.Set {
		mode = payloadModeFull
	}
	if mode == payloadModeCols && !raw.Columns.Set {
		if !v.hasPriorCorrection(raw.Mode, payloadColumnsKey) {
			v.reportStructured(R32, raw.Mode, semanticFault(
				"payload.columns is required when payload.mode is columns"))
		}
	}
	if mode != payloadModeCols && raw.Columns.Set {
		v.reportStructured(R32, raw.Columns, semanticFault(
			"payload.columns is legal only when payload.mode is columns"))
	}
	if mode != payloadModeFull && raw.Exclude.Set {
		v.reportStructured(R33, raw.Exclude, semanticFault(
			"payload.exclude is legal only when payload.mode is full"))
	}
}

func (v *validatePass) validateName(rule RuleID, named Str) {
	if !named.Valid() {
		return
	}
	defect := nameRuleDefect(named.value)
	v.reject(rule, named, defect != nameOK,
		semanticFault(fmt.Sprintf("must match %s: %s", namePattern, defect)))
}

func (v *validatePass) between(rule RuleID, value Int, low, high int) {
	v.reject(rule, value, value.value < low || value.value > high,
		semanticFault(fmt.Sprintf("must be in the range %d to %d", low, high)))
}

func (v *validatePass) validateConcurrencyRange(rule RuleID, value Int) {
	v.reject(rule, value, !concurrencyCanParticipateInComparison(value.value),
		semanticFault(fmt.Sprintf("must be in the range %d to %d", minConcurrency, maxConcurrency)))
}

func (v *validatePass) atLeast(rule RuleID, value Int, low int) {
	v.reject(rule, value, value.value < low, semanticFault(fmt.Sprintf("must be at least %d", low)))
}

func (v *validatePass) positiveInt(rule RuleID, value Int) {
	v.reject(rule, value, value.value <= 0, semanticFault("must be greater than zero"))
}

func (v *validatePass) positiveDuration(rule RuleID, value Dur) {
	v.reject(rule, value, value.value <= 0, semanticFault("must be greater than zero"))
}

func (v *validatePass) oneOf(rule RuleID, value Str, permitted []string) {
	v.reject(rule, value, !slices.Contains(permitted, value.value),
		semanticFault("must be one of "+readableList(permitted)))
}

func semanticFault(message string) fault {
	return fault{message: message}
}

// reject is the uniform Valid gate. It is deliberately above every constraint: absent, null and
// conversion-refused values have no semantic value to judge and are left to the stage that owns them.
func (v *validatePass) reject(rule RuleID, value validPositioned, broken bool, why fault) {
	if !value.Valid() || !broken {
		return
	}
	reportSemantic(&v.stage, rule, anchorSet{value: value}, why)
}
