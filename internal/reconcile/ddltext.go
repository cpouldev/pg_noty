package reconcile

import (
	"fmt"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// This package's only DDL writer renders the drop side that source.ObjectSet deliberately does
// not carry. Catalog-read names are untrusted bytes too, so they take schema's quoting path.
func dropTriggerText(target TargetReading, name string) (string, error) {
	trigger, err := ddlQuoted("trigger", name)
	if err != nil {
		return "", err
	}
	table, err := ddlQualified("target table", target.Schema, target.Table)
	if err != nil {
		return "", err
	}
	return "DROP TRIGGER " + trigger + " ON " + table, nil
}

func dropFunctionText(serviceSchema, name string) (string, error) {
	function, err := ddlQualified("function", serviceSchema, name)
	if err != nil {
		return "", err
	}
	return "DROP FUNCTION " + function + "()", nil
}

func ddlQuoted(subject, name string) (string, error) {
	if fault := schema.WhyUnusable(name); fault != schema.IdentifierOK {
		return "", fmt.Errorf("%s %q is unusable: %s", subject, name, fault)
	}
	quoted, fault := schema.Quoted(name)
	if fault != schema.IdentifierOK {
		return "", fmt.Errorf("%s %q is unusable: %s", subject, name, fault)
	}
	return quoted, nil
}

func ddlQualified(subject, schemaName, name string) (string, error) {
	if fault := schema.WhyUnusable(schemaName); fault != schema.IdentifierOK {
		return "", fmt.Errorf("%s schema %q is unusable: %s", subject, schemaName, fault)
	}
	if fault := schema.WhyUnusable(name); fault != schema.IdentifierOK {
		return "", fmt.Errorf("%s name %q is unusable: %s", subject, name, fault)
	}
	qualified, fault := schema.Qualified(schemaName, name)
	if fault != schema.IdentifierOK {
		return "", fmt.Errorf("%s %q.%q is unusable: %s", subject, schemaName, name, fault)
	}
	return qualified, nil
}
