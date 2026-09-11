// Command terraform-provider-nivis-tunnel serves the nixos_activation resource.
//
// nivis resolves providers by filesystem path, exactly as it does for
// terraform-provider-hcloudimage, so no registry publication is needed. The
// repository is named for the registry convention only so that publishing later
// requires no rename.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/nivis-project/terraform-provider-nivis-tunnel/internal/provider"
)

var version = "dev"

func main() {
	debug := flag.Bool("debug", false,
		"run with support for debuggers, printing the reattach configuration")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	err := providerserver.Serve(context.Background(), func() fwprovider.Provider {
		return provider.New(version)
	}, providerserver.ServeOpts{
		// Matches what a dev override or a nivis filesystem path resolves to.
		Address: "registry.opentofu.org/nivis-project/nivis-tunnel",
		Debug:   *debug,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "terraform-provider-nivis-tunnel: %v\n", err)
		os.Exit(1)
	}
}
