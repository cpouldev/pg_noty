package cli

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"

	"github.com/cpouldev/pg_noty/internal/reconcile"
)

// This file is how the CLI answers the approval question: what the two flags mean, whether a
// terminal is present to ask at, and the prompt itself. internal/reconcile parses no flag, detects
// no terminal and prompts for nothing, so the whole answer is here.

func approvalFor(opts *rootOptions, interactive bool) reconcile.Approval {
	if opts == nil {
		return reconcile.Approval{Interactive: interactive}
	}
	return reconcile.Approval{
		DestructionPermitted: opts.AllowDelete,
		Approved:             opts.AutoApprove,
		Interactive:          interactive,
	}
}

func approvalReason(plan reconcile.PlanResult, approval reconcile.Approval) string {
	if plan.Destructive() && !approval.DestructionPermitted {
		return "destruction permission"
	}
	if !approval.Approved && !approval.Interactive {
		return "approval"
	}
	return "proceed"
}

func confirm(ctx context.Context, in io.Reader, out io.Writer) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return false
	default:
	}
	_, _ = io.WriteString(out, "Apply destructive changes? [y/N] ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && len(line) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes")
}

func stdoutIsInteractive() bool {
	info, err := os.Stdout.Stat()
	return err == nil && interactiveFromInfo(info)
}

func interactiveFromInfo(info os.FileInfo) bool {
	return info != nil && info.Mode()&os.ModeCharDevice != 0
}
