package reconcile

import (
	"context"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/jackc/pgx/v5"
)

func objectsForPlan(ctx context.Context, tx pgx.Tx, cfg config.Config, plan PlanResult) ([]applyObject, error) {
	readings, err := readRegistries(ctx, tx, cfg.Database.Schema)
	if err != nil {
		return nil, err
	}
	recorded, listeners := registryByName(readings), configuredByName(cfg.Listeners)
	objects := make([]applyObject, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		object, err := objectForAction(
			ctx,
			tx,
			cfg,
			action,
			listeners[action.Pair.Listener],
			recorded[action.Pair.Listener],
		)
		if err != nil {
			return nil, err
		}
		if action.Kind != ActionRename && !(action.Kind == ActionDrop && listeners[action.Pair.Listener].Name == "" && len(recorded[action.Pair.Listener].Triggers) == 0) {
			objects = append(objects, object)
		}
	}
	return objects, nil
}

func objectForAction(
	ctx context.Context,
	tx pgx.Tx,
	cfg config.Config,
	action Action,
	listener config.Listener,
	recorded registryReading,
) (applyObject, error) {
	if listener.Name == "" {
		listener = config.Listener{
			Name: recorded.Listener.Name, Trigger: config.TriggerSpec{Table: recorded.Listener.TargetTable},
		}
	}
	prior := &recorded.Listener
	if prior.Name == "" {
		prior = nil
	}
	resolution, err := resolveListenerTarget(ctx, NewCatalog(tx), listener, prior)
	if err != nil || resolution.Refusal != nil {
		return applyObject{}, err
	}
	trigger, function := recordedNames(recorded, action.Pair.Operation)
	if action.Kind == ActionDrop {
		return withDropOfWhatTheCatalogHolds(
			ctx, tx, applyObject{
				Target:        resolution.Target,
				ServiceSchema: cfg.Database.Schema, TriggerName: trigger, FunctionName: function, Drop: true,
			},
		)
	}
	set, found, err := setForAction(cfg, listener, resolution.Target, action.Pair.Operation)
	if err != nil {
		return applyObject{}, err
	}
	if found {
		trigger, function = set.TriggerName, set.FunctionName
	}
	object := applyObject{
		Target: resolution.Target, ServiceSchema: cfg.Database.Schema, TriggerName: trigger, FunctionName: function,
	}
	if action.Kind == ActionCreate || action.Kind == ActionReplace {
		object.Creates = []string{
			set.CreateFunction, set.RevokeExecute, set.CommentFunction, set.CreateTrigger, set.CommentTrigger,
		}
	}
	object.Drop = action.Kind == ActionDisable || action.Kind == ActionReplace && resolution.Outcome != targetDroppedAndRecreated
	if !object.Drop {
		return object, nil
	}
	return withDropOfWhatTheCatalogHolds(ctx, tx, object)
}

// withDropOfWhatTheCatalogHolds clears the drop side for an object the catalog no longer holds. A
// plan's evidence that an object exists is its registry row, and another session can remove the
// object between this run's readings and its statements -- a hand-drop, or a concurrent destroy.
// ddltext.go renders both drops unhedged, naming no existence check, deliberately and pinned by
// TestDropTextsCarryNoIfExistsHedge; so a drop rendered for an object that has already
// gone aborts the whole transaction and takes the registry cleanup this run legitimately owed with
// it -- leaving every later apply to fail the same way.
//
// The reading is trigger-anchored because this package is: readObservedPair reaches the function
// through the trigger's OID and Catalog offers no by-name function read, so a function whose trigger
// is gone is a state nothing here models. It is also the state the server makes hardest to reach --
// it refuses to drop a function a trigger still depends on, and a cascading drop removes that
// trigger too.
//
// Every drop-side action routes through here, not only the removed-listener one that reproduced it:
// a disable and a replace take their object names from the same registry row and inherit the same
// staleness.
func withDropOfWhatTheCatalogHolds(ctx context.Context, tx pgx.Tx, object applyObject) (applyObject, error) {
	_, held, err := NewCatalog(tx).ReadTrigger(ctx, object.Target.OID, object.TriggerName)
	if err != nil {
		return applyObject{}, err
	}
	object.Drop = held
	return object, nil
}

func recordedNames(recorded registryReading, operation string) (string, string) {
	for _, candidate := range recorded.Triggers {
		if candidate.Operation == operation {
			return candidate.TriggerName, candidate.FunctionName
		}
	}
	return "", ""
}

func setForAction(
	cfg config.Config,
	listener config.Listener,
	target TargetReading,
	operation string,
) (source.ObjectSet, bool, error) {
	compiled, err := compileListener(cfg.Instance, cfg.Database.Schema, listener, target)
	if err != nil {
		return source.ObjectSet{}, false, err
	}
	for _, candidate := range compiled.Sets {
		if candidate.Operation == operation {
			return candidate, true, nil
		}
	}
	return source.ObjectSet{}, false, nil
}
