//go:build integration

package source

import (
	"reflect"
	"testing"
	"time"
)

// The two port methods whose contract at the boundary is not an error value, and the reconciliation
// that keeps closeboundary_integration_test.go's call list closed over the port. They live beside
// each other because they answer one question -- what does every EventSource method do once Close
// has returned -- and apart from the four error-answering calls only so that neither file exceeds
// the artifact budget artifactbudget_test.go enforces.

// answeredWithoutAnError is the rest of the port. Notify hands back a channel and has no error to
// return, so its answer at the boundary is the closure a consumer selects on; Close answers nil a
// second time, which is the documented idempotence of Close.
var answeredWithoutAnError = []string{"Notify", "Close"}

// assertNotifyAfterCloseReportsTheSourceFinished is Notify's own answer at the boundary. Its caller
// provokes no notification -- no trigger is installed and no pg_notify is issued -- so a value
// readable here would be one the source invented.
func assertNotifyAfterCloseReportsTheSourceFinished(t *testing.T, source *TriggerSource) {
	t.Helper()
	channel := source.Notify()
	if channel == nil {
		t.Fatal("Notify returned a nil channel after Close, and a consumer selecting on nil blocks " +
			"for the rest of the process rather than learning the source has finished")
	}
	select {
	case _, open := <-channel:
		if open {
			t.Error("Notify's channel yielded a wake-up after Close, and nothing in this case " +
				"notified anything")
		}
	case <-time.After(time.Second):
		t.Error("the channel Notify returns after Close is still open, so a consumer selecting on it " +
			"never learns the source has finished")
	}
}

// assertASecondCloseReportsNoFailure drives Close off the test goroutine because its idempotent path
// waits on closeDone: a Close that closed that channel on the wrong path would hang here rather than
// fail, and the hang would be charged to whichever case the package timeout killed.
func assertASecondCloseReportsNoFailure(t *testing.T, source *TriggerSource) {
	t.Helper()
	returned := make(chan error, 1)
	go func() { returned <- source.Close() }()
	select {
	case err := <-returned:
		if err != nil {
			t.Errorf("a second Close returned %v, want nil: Close is idempotent and a "+
				"deferred Close must not report a failure that did not happen", err)
		}
	case <-time.After(theCloseDeadline):
		t.Fatalf("a second Close did not return within %s", theCloseDeadline)
	}
}

// TestEveryPortMethodHasAnAnswerAfterClose keeps the call list closed over the port rather than over
// a copy of it. A method added to EventSource without a row is a method whose answer at the boundary
// nothing states, and a name here the port does not declare is a case exercising nothing.
func TestEveryPortMethodHasAnAnswerAfterClose(t *testing.T) {
	skipIfShort(t)
	port := reflect.TypeOf((*EventSource)(nil)).Elem()
	answered := map[string]bool{}
	for _, name := range answeredWithoutAnError {
		answered[name] = true
	}
	for name := range callsAnsweringWithAnError {
		answered[name] = true
	}
	for index := range port.NumMethod() {
		if !answered[port.Method(index).Name] {
			t.Errorf("EventSource.%s is exercised by no after-Close case, so its answer at the "+
				"boundary is whatever it happens to be", port.Method(index).Name)
		}
	}
	if len(answered) != port.NumMethod() {
		t.Errorf("%d method names are answered after Close and the port declares %d", len(answered),
			port.NumMethod())
	}
}
