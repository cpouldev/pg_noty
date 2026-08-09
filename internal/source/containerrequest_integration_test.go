//go:build integration

package source

import (
	"reflect"
	"strings"
	"testing"
)

const (
	// theHarnessContainerPort is the container-side port postgres.Run exposes. Never a host port.
	theHarnessContainerPort = "5432/tcp"
	// aFixedHostPort and aFixedContainerName are what the reader's control plants in a synthetic
	// request. Neither is written anywhere the harness reads.
	aFixedHostPort      = "5432"
	aFixedContainerName = "pgnoty-source-harness"
)

// TestTheHarnessContainerRequestFixesNoHostPortAndNoContainerName is SC-4's authority. It reads the
// container request testcontainers-go was actually handed by the sole postgres.Run call --
// recorded live by recordingTheRequest in testmain_integration_test.go -- rather than the source
// text that built it, so a name or a port reaching the request through a const, through a
// ContainerRequest literal, through testcontainers.CustomizeRequest or through a
// HostConfigModifier is answered by the same assertion as the spelling anyone thought of.
//
// The reason the criterion exists is a collision between processes, not a matter of style. Slot S7
// runs three of these container-backed harnesses concurrently and slot S8 runs two. Docker refuses
// to create a second container under a name that is already taken, and refuses to bind a host port
// another process holds, so either would turn concurrent steps into a failure that reads as a
// flaky daemon rather than as a harness that asked for something it cannot have.
func TestTheHarnessContainerRequestFixesNoHostPortAndNoContainerName(t *testing.T) {
	skipIfShort(t)
	facts := readContainerRequest(theRecordedHarnessRequest(t))

	// The request read has to be the one that started the container. A value whose every field
	// came back zero fixes nothing by definition, so the assertion below would pass over an empty
	// recording, over a recording taken before postgres.Run's own customizers ran, and over a
	// reader that stopped finding any field at all
	// (.claude/rules/count-the-population-a-vacuity-guard-guards.md).
	switch {
	case facts.image != harnessImage:
		t.Fatalf("the recorded request carries image %q, want the harness's %q", facts.image, harnessImage)
	case len(facts.exposedPorts) == 0:
		t.Fatal("the recorded request exposes no port, so it is not the request postgres.Run built")
	case !facts.waits:
		t.Fatal("the recorded request carries no wait strategy, so it was recorded before the " +
			"harness's own customizers ran")
	}

	if fixed := fixedContainerSettings(facts); len(fixed) != 0 {
		t.Fatalf("the harness container request fixes %v; three of these harnesses run concurrently "+
			"in slot S7 and two in S8, and docker cannot give any of them twice", fixed)
	}
}

// TestTheContainerRequestReaderReadsAFixedNameAndAFixedHostPortBackOut gives the reader its own
// falsifiability, over the type it actually reads. The predicate's controls in
// containerrequestcontrols_test.go run on hand-built facts and would all pass if
// readContainerRequest returned an empty value for every request it was handed, which is precisely
// the failure that would make the authority test above green while the harness fixed both
// (.claude/rules/falsify-the-assertion-not-a-copy-of-it.md).
//
// The synthetic request is built from the recorded request's own type, so it is
// testcontainers-go's struct rather than a local imitation of it, and the type is still never named
// here.
func TestTheContainerRequestReaderReadsAFixedNameAndAFixedHostPortBackOut(t *testing.T) {
	skipIfShort(t)
	facts := readContainerRequest(aRequestFixing(t, aFixedContainerName, aFixedHostPort))

	if len(facts.unreadable) != 0 {
		t.Fatalf("the reader could not read %v out of testcontainers-go's own request type", facts.unreadable)
	}
	if facts.name != aFixedContainerName {
		t.Errorf("the reader read the container name %q, want the planted %q", facts.name, aFixedContainerName)
	}
	if len(facts.hostPortBindings) != 1 || !strings.Contains(facts.hostPortBindings[0], aFixedHostPort) {
		t.Fatalf("the reader read the host port bindings %v, want one naming the planted %q",
			facts.hostPortBindings, aFixedHostPort)
	}
	// Both halves of the binding have to be named. The key of PortBindings is a struct in this moby
	// version: reflect.Value.String answers a "<network.Port Value>" placeholder for one, and a key
	// aPortKey failed to parse renders as "invalid port". Either would send whoever reads the
	// diagnostic looking for a binding no port names, so the container side is asserted too.
	if !strings.Contains(facts.hostPortBindings[0], theHarnessContainerPort) {
		t.Errorf("the reader rendered the binding as %q, which names no container port; want the "+
			"planted %q", facts.hostPortBindings[0], theHarnessContainerPort)
	}
	if fixed := fixedContainerSettings(facts); len(fixed) != 2 {
		t.Fatalf("fixedContainerSettings = %v, want one clause for the planted name and one for the "+
			"planted host port", fixed)
	}
}

// theRecordedHarnessRequest is the request the harness handed testcontainers-go, refused rather
// than defaulted when nothing was recorded: an absent recording must not read as a request that
// fixes nothing.
func theRecordedHarnessRequest(t *testing.T) any {
	t.Helper()
	if harnessContainerRequest == nil {
		t.Fatal("the harness recorded no container request, so SC-4 is asserted against nothing; " +
			"recordingTheRequest is no longer wrapping an option postgres.Run applies")
	}
	return harnessContainerRequest
}

// aRequestFixing is a container request of the library's own type that fixes both things SC-4
// forbids -- a container name, and a host port bound through a HostConfigModifier, which is the
// route the source-text heuristic cannot see at all.
func aRequestFixing(t *testing.T, name, hostPort string) any {
	t.Helper()
	held := reflect.New(reflect.TypeOf(theRecordedHarnessRequest(t)).Elem()).Elem()
	settableField(t, held, "Name").SetString(name)

	modifier := settableField(t, held, "HostConfigModifier")
	modifier.Set(reflect.MakeFunc(modifier.Type(), func(arguments []reflect.Value) []reflect.Value {
		bindOneHostPort(arguments[0].Elem().FieldByName("PortBindings"), hostPort)
		return nil
	}))
	return held.Addr().Interface()
}

// settableField refuses a field this control can no longer plant into, so a renamed field is a
// sentence rather than a panic from inside reflect.
func settableField(t *testing.T, held reflect.Value, name string) reflect.Value {
	t.Helper()
	field := held.FieldByName(name)
	if !field.IsValid() || !field.CanSet() {
		t.Fatalf("the request type has no settable %s, so this control cannot plant one into it", name)
	}
	return field
}

// bindOneHostPort writes `{theHarnessContainerPort: [{HostPort: hostPort}]}` into a host config's
// PortBindings, building the map, the slice and the binding from the field's own types.
func bindOneHostPort(bindings reflect.Value, hostPort string) {
	binding := reflect.New(bindings.Type().Elem().Elem()).Elem()
	binding.FieldByName("HostPort").SetString(hostPort)

	written := reflect.MakeMap(bindings.Type())
	written.SetMapIndex(aPortKey(bindings.Type().Key(), theHarnessContainerPort),
		reflect.Append(reflect.MakeSlice(bindings.Type().Elem(), 0, 1), binding))
	bindings.Set(written)
}

// aPortKey builds one PortBindings key of the map's own key type. The vendored moby API spells a
// port as a struct whose fields are unexported and whose only constructor parses text, so the key
// cannot be converted from a string and is unmarshalled instead; an older spelling of the same
// field is a named string type, which converts.
func aPortKey(keyType reflect.Type, port string) reflect.Value {
	if keyType.Kind() == reflect.String {
		return reflect.ValueOf(port).Convert(keyType)
	}
	key := reflect.New(keyType)
	if unmarshal := key.MethodByName("UnmarshalText"); unmarshal.IsValid() {
		unmarshal.Call([]reflect.Value{reflect.ValueOf([]byte(port))})
	}
	return key.Elem()
}
