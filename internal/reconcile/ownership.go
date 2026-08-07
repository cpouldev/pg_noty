package reconcile

// Disagreement reports why the registry row, the catalog comment and the instance did not prove
// ownership. Its zero value deliberately reports no catalog state, following
// internal/schema/ownershipmarker.go's markerStates discipline: an unread proof must never look like
// a proof that agreed.
type Disagreement string

const (
	DisagreementMarkerAbsent    Disagreement = "marker_absent"
	DisagreementForeignInstance Disagreement = "foreign_instance"
	DisagreementAnotherPair     Disagreement = "another_pair"
	DisagreementNoRegistryRow   Disagreement = "no_registry_row"
)

// disagreements is a value so tests pin this closed safety vocabulary at four entries.
var disagreements = []Disagreement{
	DisagreementMarkerAbsent,
	DisagreementForeignInstance,
	DisagreementAnotherPair,
	DisagreementNoRegistryRow,
}

// RegistryRow is the L1 subset of a recorded listener-trigger pair used by the ownership proof.
type RegistryRow struct {
	Present   bool
	Listener  string
	Operation string
}

// CatalogObject is the L1 catalog reading used by the ownership proof. Marker nil means no comment.
// Kind and Identity retain which independently owned object the catalog reported.
type CatalogObject struct {
	Kind     string
	Identity string
	Marker   *string
}

// Ownership is the result of checking a recorded pair, a catalog comment and this instance together.
type Ownership struct {
	Owned        bool
	Disagreement Disagreement
	Found        string
}

// DetermineOwnership refuses whenever any part of the three-part proof disagrees.
func DetermineOwnership(recorded RegistryRow, found CatalogObject, instance string) Ownership {
	marker, readable := parseCatalogMarker(found.Marker)
	result := Ownership{Found: markerValue(found.Marker)}
	if !recorded.Present {
		result.Disagreement = DisagreementNoRegistryRow
		return result
	}
	if !readable {
		result.Disagreement = DisagreementMarkerAbsent
		return result
	}
	if marker.Instance != instance {
		result.Disagreement = DisagreementForeignInstance
		return result
	}
	if marker.Listener != recorded.Listener || marker.Operation != recorded.Operation {
		result.Disagreement = DisagreementAnotherPair
		return result
	}
	result.Owned = true
	return result
}

func markerValue(marker *string) string {
	if marker == nil {
		return ""
	}
	return *marker
}
