package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// stateSetter is what Create and Update share of their responses' state, so
// one helper can write either.
type stateSetter interface {
	Set(ctx context.Context, val interface{}) diag.Diagnostics
}

type diagnosticsAppender interface {
	AddError(summary, detail string)
	Append(in ...diag.Diagnostic)
}
