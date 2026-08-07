package source

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// This file is SC-4's authority: the integration harness must bind no fixed host port and no fixed
// container name, and the question is settled here by reading the request testcontainers-go was
// actually handed rather than the source text that built it. The reason it matters is a collision
// between processes rather than a matter of style -- slot S7 runs three of these container-backed
// harnesses concurrently and S8 runs two, docker refuses a second container under a name already
// taken, and a second bind of a held host port fails outright.
//
// containersettingscan_test.go holds the cheap second line, whose enumeration is its own rather
// than the library's; the assertions that drive what is here are
// TestTheHarnessContainerRequestFixesNoHostPortAndNoContainerName and
// TestTheContainerRequestReaderReadsAFixedNameAndAFixedHostPortBackOut.

// containerRequestFacts is the part of a testcontainers container request SC-4 ranges over, lifted
// out of the live request so the predicate below is ordinary Go and its controls can run in the
// untagged tier. image and waits carry no policy: they are what the authority test reads to prove
// the value it was handed is the request that started the container rather than a zero one.
//
// unreadable names every field the reader could not find or found with another shape. A request
// whose fields were renamed has to fail loudly rather than answer "nothing is fixed"
// (.claude/rules/scan-result-sentinels.md).
type containerRequestFacts struct {
	image            string
	name             string
	reuse            bool
	exposedPorts     []string
	hostPortBindings []string
	waits            bool
	unreadable       []string
}

// readContainerRequest reads one *testcontainers.GenericContainerRequest without naming its type.
// Reflection rather than an import, for the reason recordingTheRequest records: importing
// testcontainers-go directly promotes it from an indirect to a direct requirement of go.mod, which
// internal/config/releasehygiene_test.go refuses.
//
// Every field is taken by name from the library's own struct, so this reader's authority is the
// request rather than a list of option spellings kept here. It is driven over a request of that
// real type by TestTheContainerRequestReaderReadsAFixedNameAndAFixedHostPortBackOut.
func readContainerRequest(request any) containerRequestFacts {
	var facts containerRequestFacts
	pointer := reflect.ValueOf(request)
	if pointer.Kind() != reflect.Pointer || pointer.IsNil() || pointer.Elem().Kind() != reflect.Struct {
		return containerRequestFacts{unreadable: []string{"the recorded request is not a pointer to a struct"}}
	}

	held := pointer.Elem()
	if field, ok := fieldOf(held, "Image", reflect.String, &facts.unreadable); ok {
		facts.image = field.String()
	}
	if field, ok := fieldOf(held, "Name", reflect.String, &facts.unreadable); ok {
		facts.name = field.String()
	}
	if field, ok := fieldOf(held, "Reuse", reflect.Bool, &facts.unreadable); ok {
		facts.reuse = field.Bool()
	}
	if field, ok := fieldOf(held, "ExposedPorts", reflect.Slice, &facts.unreadable); ok {
		for index := range field.Len() {
			facts.exposedPorts = append(facts.exposedPorts, field.Index(index).String())
		}
	}
	if field, ok := fieldOf(held, "WaitingFor", reflect.Interface, &facts.unreadable); ok {
		facts.waits = !field.IsNil()
	}
	if field, ok := fieldOf(held, "HostConfigModifier", reflect.Func, &facts.unreadable); ok {
		facts.hostPortBindings = hostPortBindingsOf(field, &facts.unreadable)
	}
	return facts
}

// fieldOf reads one named field of the request. A field that is absent, or present with another
// kind, is recorded rather than skipped: either way the reader can no longer answer the question it
// was written for, and an empty answer here reads as "nothing was fixed".
func fieldOf(held reflect.Value, name string, kind reflect.Kind, unreadable *[]string) (reflect.Value, bool) {
	field := held.FieldByName(name)
	switch {
	case !field.IsValid():
		*unreadable = append(*unreadable, name+" is no longer a field of the request")
	case field.Kind() != kind:
		*unreadable = append(*unreadable, name+" is a "+field.Kind().String()+", not a "+kind.String())
	default:
		return field, true
	}
	return reflect.Value{}, false
}

// hostPortBindingsOf applies a HostConfigModifier to a host config docker has not touched and
// reports every port binding it writes. A fresh host config binds nothing, so any entry at all is a
// host port this harness asked for by name -- which is why the criterion reads "nil, or leaves
// PortBindings empty" rather than "binds no particular port".
func hostPortBindingsOf(modifier reflect.Value, unreadable *[]string) []string {
	if modifier.IsNil() {
		return nil
	}
	shape := modifier.Type()
	if shape.NumIn() != 1 || shape.In(0).Kind() != reflect.Pointer || shape.In(0).Elem().Kind() != reflect.Struct {
		*unreadable = append(*unreadable, "HostConfigModifier no longer takes one pointer to a struct")
		return nil
	}

	fresh := reflect.New(shape.In(0).Elem())
	modifier.Call([]reflect.Value{fresh})
	bindings, ok := fieldOf(fresh.Elem(), "PortBindings", reflect.Map, unreadable)
	if !ok {
		return nil
	}

	var written []string
	for _, port := range bindings.MapKeys() {
		written = append(written, boundHostPorts(port, bindings.MapIndex(port), unreadable)...)
	}
	slices.Sort(written)
	return written
}

// boundHostPorts names every host port one PortBindings entry writes. The key is rendered through
// fmt rather than through reflect.Value.String, because the vendored moby API spells a port as a
// struct with unexported fields and its own Stringer -- reflect.Value.String answers
// "<network.Port Value>" for that, which would name no port at all in a diagnostic.
func boundHostPorts(port, bound reflect.Value, unreadable *[]string) []string {
	if bound.Kind() != reflect.Slice {
		*unreadable = append(*unreadable, "PortBindings no longer maps a port to a slice")
		return nil
	}

	var written []string
	for index := range bound.Len() {
		host, ok := fieldOf(bound.Index(index), "HostPort", reflect.String, unreadable)
		if !ok {
			continue
		}
		written = append(written, fmt.Sprint(port.Interface())+" to host port "+strconv.Quote(host.String()))
	}
	return written
}

// fixedContainerSettings is every setting in one container request that two concurrent harnesses
// cannot share, one clause per reason so a control can name which one fired rather than only that
// something did (.claude/rules/isolate-each-clause-of-a-multi-clause-guard.md).
//
// The exposed-port clause tests for a colon because docker spells a host binding
// `<host>:<container>` or `<ip>:<host>:<container>`; a bare `5432/tcp` names a container port only
// and lets docker choose the host side.
func fixedContainerSettings(facts containerRequestFacts) []string {
	var fixed []string
	for _, field := range facts.unreadable {
		fixed = append(fixed, "an unreadable request: "+field)
	}
	if facts.name != "" {
		fixed = append(fixed, "a fixed container name "+strconv.Quote(facts.name))
	}
	if facts.reuse {
		fixed = append(fixed, "container reuse, which docker can only honour for a named container")
	}
	for _, exposed := range facts.exposedPorts {
		if strings.Contains(exposed, ":") {
			fixed = append(fixed, "a fixed host port in the exposed port "+strconv.Quote(exposed))
		}
	}
	for _, binding := range facts.hostPortBindings {
		fixed = append(fixed, "a host port bound by HostConfigModifier: "+binding)
	}
	return fixed
}
