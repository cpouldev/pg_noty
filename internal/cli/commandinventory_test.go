package cli

import (
	"slices"
	"testing"

	"github.com/spf13/cobra"
)

var expectedCommandInventory = []string{"plan", "apply", "run", "status", "events", "destroy", "validate", "grants", "repair-default-partition", "bootstrap"}

func TestCommandInventoryIsClosedAndSizePinned(t *testing.T) {
	root := newRoot(&rootOptions{})
	got := make([]string, 0, len(root.Commands()))
	for _, command := range root.Commands() {
		got = append(got, command.Name())
	}
	slices.Sort(got)
	want := slices.Clone(expectedCommandInventory)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("commands=%v, want %v", got, want)
	}
	events, _, err := root.Find([]string{"events"})
	if err != nil || events == nil || !slices.Equal(commandNames(events.Commands()), []string{"list", "retry"}) {
		t.Fatalf("events children=%v, want list/retry", commandNames(events.Commands()))
	}
}

func commandNames(commands []*cobra.Command) []string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.Name())
	}
	slices.Sort(names)
	return names
}
