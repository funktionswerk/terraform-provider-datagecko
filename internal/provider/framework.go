package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// clientHolder is the one Configure every resource shares: embed it, and
// r.client is the provider's client once Terraform has configured the provider.
type clientHolder struct {
	client *Client
}

func (h *clientHolder) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if client := takeClient(req.ProviderData, &resp.Diagnostics); client != nil {
		h.client = client
	}
}

// takeClient is the provider data as the client, or nil before the provider
// is configured; anything else is a programming error, reported once.
func takeClient(providerData any, diags *diag.Diagnostics) *Client {
	if providerData == nil {
		return nil
	}
	client, ok := providerData.(*Client)
	if !ok {
		diags.AddError("Unexpected Provider Data Type", fmt.Sprintf("Expected *Client, got: %T", providerData))
		return nil
	}
	return client
}

func isNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsNotFound()
}

func isConflict(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsConflict()
}

// apiMessage is the API's own message when the error carries one.
func apiMessage(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	return err.Error()
}

// refuseExisting is the one answer to a row that already exists on create:
// it is never adopted, and the remedy is named.
func refuseExisting(diags *diag.Diagnostics, summary, detail string) {
	diags.AddError(summary, detail+" — declared outside Terraform; import it.")
}

// finishDelete reports a delete's outcome: a row already gone is success.
func finishDelete(diags *diag.Diagnostics, what string, err error) {
	if err == nil || isNotFound(err) {
		return
	}
	diags.AddError("Error "+what, err.Error())
}
