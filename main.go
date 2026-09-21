package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider"
)

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
