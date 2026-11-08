package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider"
)

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 generate --provider-name datagecko

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/funktionswerk/datagecko",
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
