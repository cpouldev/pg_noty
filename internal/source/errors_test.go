package source

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSourceSentinelsAreMatchableAndDistinct(t *testing.T) {
	infrastructure := errors.New("database unreachable")
	for _, sentinel := range []error{ErrEventNotClaimed, ErrSourceClosed} {
		if !errors.Is(fmt.Errorf("wrapped: %w", sentinel), sentinel) {
			t.Errorf("errors.Is does not match wrapped %v", sentinel)
		}
		if errors.Is(sentinel, infrastructure) || errors.Is(infrastructure, sentinel) {
			t.Errorf("sentinel %v matches infrastructure failure", sentinel)
		}
	}
	if errors.Is(ErrEventNotClaimed, ErrSourceClosed) || errors.Is(ErrSourceClosed, ErrEventNotClaimed) {
		t.Fatal("the two source sentinels are not distinct")
	}
}

func TestCancellationClassificationIsDocumented(t *testing.T) {
	doc := string(sourceBytes(t, "errors.go"))
	for _, phrase := range []string{"context.Canceled", "errors.Is"} {
		if !strings.Contains(doc, phrase) {
			t.Errorf("errors.go does not document %s classification", phrase)
		}
	}
}
