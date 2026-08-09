package schema

// theDrain is the one file that moves rows out of the DEFAULT partition. The single-authority scans
// hold it, and the other authority names, to account.
const theDrain = "repair.go"

// The single-authority file names the surviving trust-boundary scans hold to account: one file may
// read the system catalogs, one may take the advisory lock, one may bound a statement's execution.
// The rest name the driver-reaching sources errorelisionscan sweeps, so no driver error escapes
// this package unelided.
const (
	theObservationAuthority      = "catalog.go"
	theLockAuthority             = "advisorylock.go"
	theBoundedExecutionAuthority = "ddl.go"
	theRetentionPass             = "retention.go"
	theRunner                    = "runner.go"
	theRunnerLedger              = "runnerledger.go"
)

// exactL1Files is this package's L1 set: the files holding the arithmetic and the object contract,
// reaching no driver, no cancellation and no I/O. identifierscan holds the L3 files against it.
var exactL1Files = []string{
	"doc.go", "horizon.go", "objectname.go", "options.go", "partition.go", "plan.go", "stats.go",
}
