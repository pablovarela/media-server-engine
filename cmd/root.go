package cmd

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/github"
	"github.com/pablovarela/media-server-engine/internal/images"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/secrets"
	"github.com/pablovarela/media-server-engine/internal/selfupdate"
	"github.com/pablovarela/media-server-engine/internal/version"
)

type Dependencies struct {
	Build       version.Build
	Update      updater
	Engine      fs.FS
	Environment func(string) string
	Home        string
	Host        installation.Host
	Decrypt     secrets.Decrypter
	Compose     func(out, errOut io.Writer) (composeRunner, error)
	HTTP        *http.Client
	Images      func() (images.Client, error)
}

func NewRootCommand(deps Dependencies) *cobra.Command {
	root := &cobra.Command{
		Use:           "mse",
		Short:         "Run a media-server installation",
		Version:       deps.Build.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.PersistentFlags().String("installation", "", "the installation to use, when there are several (or MSE_INSTALLATION)")
	root.AddCommand(
		newVersionCommand(deps.Build),
		newUpdateCommand(deps.Build, deps.Update),
		newURLsCommand(deps),
		newLoginsCommand(deps),
		newHomepageCommand(deps),
		newPruneStackImagesCommand(deps),
		newProjectCommand(deps, "stack", "Run the media server's containers", compose.Stack),
		newProjectCommand(deps, "monitoring", "Run the monitoring containers", compose.Monitoring),
	)
	return root
}

func Execute(engine fs.FS) int {
	ctx, stop := interruptible()
	defer stop()
	home, err := os.UserHomeDir()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "mse: %v\n", err)
		return 1
	}
	client := github.NewClient(github.SystemTokenSource(), &http.Client{Timeout: 5 * time.Minute})
	deps := Dependencies{
		Build:       version.Current(),
		Update:      selfupdate.New(client, os.Executable, selfupdate.SystemVersionReader{}),
		Engine:      engine,
		Environment: os.Getenv,
		Home:        home,
		Host:        installation.SystemHost(),
		Decrypt:     secrets.Sops(installation.BasesFrom(os.Getenv, home).Config),
		Compose: func(out, errOut io.Writer) (composeRunner, error) {
			return compose.NewRunner(out, errOut)
		},
		HTTP:   &http.Client{Timeout: 30 * time.Second},
		Images: images.NewDocker,
	}
	return run(ctx, NewRootCommand(deps), os.Args[1:])
}

func interruptible() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx, stop
}

func run(ctx context.Context, root *cobra.Command, args []string) int {
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintf(root.ErrOrStderr(), "mse: %v\n", err)
		return 1
	}
	return 0
}
