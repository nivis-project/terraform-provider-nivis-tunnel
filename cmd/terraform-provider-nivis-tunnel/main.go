// Command terraform-provider-nivis-tunnel is a placeholder plugin binary.
//
// It exists so the build gate is real before any behaviour is written. See the
// beans roadmap in the companion repository nivis-tunnel.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	fmt.Fprintf(os.Stderr, "terraform-provider-nivis-tunnel %s: not implemented yet\n", version)
	os.Exit(1)
}
