package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/version"
)

func NewRootCommand(build version.Build) *cobra.Command {
	root := &cobra.Command{
		Use:           "mse",
		Short:         "Run a media-server installation",
		Version:       build.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(newVersionCommand(build))
	return root
}

func Execute() int {
	return run(NewRootCommand(version.Current()), os.Args[1:])
}

func run(root *cobra.Command, args []string) int {
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintf(root.ErrOrStderr(), "mse: %v\n", err)
		return 1
	}
	return 0
}
