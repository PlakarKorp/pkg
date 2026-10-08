package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/PlakarKorp/kloset/caching"
	"github.com/PlakarKorp/kloset/caching/pebble"
	"github.com/PlakarKorp/kloset/kcontext"
	"github.com/PlakarKorp/kloset/logging"
	"github.com/spf13/cobra"

	"github.com/PlakarKorp/pkg/validator"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:          "validator <package.ptar>",
		Short:        "Check a packaged plakar integration",
		Long:         "Check the README, the manifest of a packaged plakar integration and every JSON Schema it references.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			v := &validator.Validator{}
			if verbose {
				v.Logger = log.New(cmd.OutOrStdout(), "", 0)
			}
			if err := validatePtar(v, args[0]); err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: ok\n", args[0])
			return err
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "log each file as it is checked")
	return cmd
}

func validatePtar(v *validator.Validator, ptar string) (err error) {
	// Using the storage kloset connector to open a ptar, it needs a state file
	cachedir, err := os.MkdirTemp("", "plakar-validator-")
	if err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(cachedir) }()

	cache := caching.NewManager(pebble.InMemoryConstructor())
	defer func() {
		if cerr := cache.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("close cache: %w", cerr)
		}
	}()

	kctx := kcontext.NewKContext()
	kctx.SetLogger(logging.NewLogger(io.Discard, io.Discard))
	kctx.SetCache(cache)
	kctx.CacheDir = cachedir

	return v.ValidatePtar(kctx, ptar)
}
