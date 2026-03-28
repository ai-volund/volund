package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set via ldflags at build time.
var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:     "volund",
		Short:   "Volund platform server",
		Version: version,
	}

	rootCmd.AddCommand(gatewayCmd())
	rootCmd.AddCommand(controlplaneCmd())
	rootCmd.AddCommand(serveCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
