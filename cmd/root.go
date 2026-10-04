package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/github"
	"github.com/pablovarela/media-server-engine/internal/selfupdate"
	"github.com/pablovarela/media-server-engine/internal/version"
)

func NewRootCommand(build version.Build, update updater) *cobra.Command {
	root := &cobra.Command{
		Use:           "mse",
		Short:         "Run a media-server installation",
		Version:       build.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(newVersionCommand(build), newUpdateCommand(build, update))
	return root
}

func Execute() int {
	client := github.NewClient(github.SystemTokenSource(), &http.Client{Timeout: 5 * time.Minute})
	update := selfupdate.New(client, os.Executable, selfupdate.SystemVersionReader{})
	return run(NewRootCommand(version.Current(), update), os.Args[1:])
}

func run(root *cobra.Command, args []string) int {
	root.SetArgs(args)
	if err := root.ExecuteContext(context.Background()); err != nil {
		_, _ = fmt.Fprintf(root.ErrOrStderr(), "mse: %v\n", err)
		return 1
	}
	return 0
}
