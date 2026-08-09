package config

import "reflect"

// This file is stage G's second half: every wrapper the decoder filled meets the source it was read
// from.
//
// **Why a walk exists at all.** The decoder constructs each wrapper itself, by reflection, and hands
// it one thing: its node. A position, though, is derived against the raw source *line* (ADR-4), and no
// node carries the text of its line or the name of its file -- so a wrapper cannot answer File() or
// Col() at the moment it is filled. The channel from a wrapper back to the pipeline is therefore the
// wrapper's own state, and the pipeline has to go and read it. That is this walk: it hands every
// wrapper beneath the raw tree the pass, and the wrapper materialises its position and reports what it
// could not read.
//
// A package-level accumulator would have removed the walk and is not an option: `Parse` is a pure
// function of its arguments and two concurrent calls must not share one, which the determinism and
// offline-operation requirements both rest on.
//
// **The division of labour, and why it is not uniform.** The walk descends *decode targets* -- the raw
// structs, and the slices and mappings holding them -- and it resolves anything that is a wrapper. It
// does not descend *into* a wrapper: a wrapper holding wrappers of its own (a list of scalars, an
// operations mapping, a header mapping) keeps them unexported and resolves them itself, because what
// it holds is its own business and reflection cannot reach an unexported field's value anyway. What
// the two container carriers hold is the opposite case -- a plain decode target whose own fields are
// wrappers -- so those fields are exported and the walk descends them.

// resolvable is a decoded value that owes its position to the source it was read from.
//
// Its method is unexported, so nothing outside this package can be resolved and no consumer can
// re-resolve a wrapper against a different source.
type resolvable interface {
	resolve(pass *decodePass)
}

// resolveEveryValue hands every wrapper beneath decoded the pass, so each of them materialises its
// position and reports its refusals.
//
// TestEveryWrapperOfADecodedDocumentIsResolved is what keeps it complete: it walks a document that
// writes every key of every level and fails on any wrapper left without a position, so a raw field
// added later cannot quietly go unresolved.
func resolveEveryValue(pass *decodePass, decoded any) {
	resolveWithin(pass, reflect.ValueOf(decoded))
}

// resolveWithin resolves held, if it is a wrapper, and then descends whatever decode target it is.
//
// Both, not either: a container carrier is a wrapper *and* holds a decode target, so resolving it and
// descending it are two things that both have to happen.
func resolveWithin(pass *decodePass, held reflect.Value) {
	if held.Kind() == reflect.Pointer {
		if held.IsNil() {
			return
		}
		held = held.Elem()
	}

	if owed, isWrapper := wrapperIn(held); isWrapper {
		owed.resolve(pass)
	}

	switch held.Kind() {
	case reflect.Struct:
		for i := range held.NumField() {
			// The walk's subject is the exported decode targets. An unexported field is either a
			// wrapper's own state or a collection of wrappers the owning wrapper resolves itself, and
			// neither is this walk's to descend.
			//
			// Removing this check changes no answer -- mutation testing found that, and it is why the
			// check is stated as intent rather than as a guard: reflection cannot read the value of
			// anything reached through an unexported field, so wrapperIn below already answers false for
			// every field this skips. What the check adds is that the walk does not descend state that
			// resolves itself, which is a claim about its subject rather than about its result.
			if held.Type().Field(i).IsExported() {
				resolveWithin(pass, held.Field(i))
			}
		}
	case reflect.Slice:
		for i := range held.Len() {
			resolveWithin(pass, held.Index(i))
		}
	}
}

// wrapperIn is the resolvable held here, and whether this is one.
//
// The address is what implements the interface, because a wrapper resolving itself writes to itself;
// a value that is not addressable is therefore not a wrapper this walk can resolve, which is the
// fail-closed direction and is why the completeness test above exists.
func wrapperIn(held reflect.Value) (resolvable, bool) {
	if !held.CanAddr() || !held.Addr().CanInterface() {
		return nil, false
	}

	owed, isWrapper := held.Addr().Interface().(resolvable)
	return owed, isWrapper
}
