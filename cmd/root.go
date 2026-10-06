package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/configure"
	"github.com/pablovarela/media-server-engine/internal/github"
	"github.com/pablovarela/media-server-engine/internal/images"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/logfile"
	"github.com/pablovarela/media-server-engine/internal/machine"
	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/secrets"
	"github.com/pablovarela/media-server-engine/internal/selfupdate"
	"github.com/pablovarela/media-server-engine/internal/version"
	"github.com/pablovarela/media-server-engine/internal/wiring"
)

type Dependencies struct {
	Build         version.Build
	Update        updater
	Engine        fs.FS
	Environment   func(string) string
	Home          string
	Host          installation.Host
	Decrypt       secrets.Decrypter
	Compose       func(tool io.Writer, outcomes *compose.Outcomes) (composeRunner, error)
	HTTP          *http.Client
	Images        func() (images.Client, error)
	Run           func(out, errOut io.Writer) commandRunner
	MachineIDFile string
	Now           func() time.Time
	Sleep         func(time.Duration)
	Pause         func(ctx context.Context, d time.Duration) error
	Interactive   func() bool
	Systemd       func() bool
	LocalTime     string
	Exec          func(path string, args []string) error
	Executable    func() (string, error)
	UnitDir       string
	ProcRoot      string
	Account       func() (string, error)
	WiringSteps   func(configarr wiring.OneOff, tool io.Writer) ([]wiring.Step, error)
	Terminal      func() bool
	Prompter      func(ctx context.Context) prompter
	Encrypt       func(config string) secrets.Encrypter
	RandomKey     func() (string, error)
	GOOS          string
	PortFree      func(machine.Port) bool
	Published     func(ctx context.Context) ([]machine.Published, error)
	Repositories  configRepositories
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
	root.PersistentFlags().BoolP("verbose", "v", false, "show the output of restic and Compose too (the log always has it)")
	root.Long = "Run a media-server installation.\n\nEvery run is logged to ~/.local/state/mse/<installation>/logs/mse.log, or ~/.local/state/mse/mse.log before an installation is known (under $XDG_STATE_HOME when it is set)."
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		verbose, _ := cmd.Flags().GetBool("verbose")
		report.From(cmd.Context()).SetVerbose(verbose)
	}
	root.AddCommand(
		newVersionCommand(deps.Build),
		newUpdateCommand(deps),
		newConfigureCommand(deps),
		newCheckMachineCommand(deps),
		newCreateCommand(deps),
		newApplyCommand(deps),
		newURLsCommand(deps),
		newLoginsCommand(deps),
		newHomepageCommand(deps),
		newPruneStackImagesCommand(deps),
		newRemoveExecutableDownloadsCommand(deps),
		newProjectCommand(deps, "stack", "Run the media server's containers", compose.Stack),
		newProjectCommand(deps, "monitoring", "Run the monitoring containers", compose.Monitoring),
	)
	root.AddCommand(newBackupCommands(deps)...)
	return root
}

func Execute(engine fs.FS) int {
	paint.Detect()
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
		Compose: func(tool io.Writer, outcomes *compose.Outcomes) (composeRunner, error) {
			return compose.NewRunner(tool, outcomes)
		},
		HTTP:          &http.Client{Timeout: 30 * time.Second},
		Images:        images.NewDocker,
		Run:           func(out, errOut io.Writer) commandRunner { return process.System{Out: out, ErrOut: errOut} },
		MachineIDFile: "/etc/machine-id",
		Now:           time.Now,
		Sleep:         time.Sleep,
		Pause:         pause,
		Interactive:   func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }, //nolint:gosec // a file descriptor fits in an int
		Systemd:       func() bool { _, err := os.Stat("/run/systemd/system"); return err == nil },
		LocalTime:     "/etc/localtime",
		Executable:    os.Executable,
		Account:       currentAccount,
		WiringSteps:   wiringSteps,
		Terminal:      bothTerminals,
		Prompter:      func(ctx context.Context) prompter { return configure.Huh{Ctx: ctx} },
		Encrypt: func(config string) secrets.Encrypter {
			return secrets.SopsEncrypter(installation.BasesFrom(os.Getenv, home).Config, filepath.Join(config, ".sops.yaml"))
		},
		RandomKey:    secrets.RandomKey,
		GOOS:         runtime.GOOS,
		PortFree:     machine.PortFree,
		Published:    machine.DockerPublished,
		Repositories: client,
		Exec:         func(path string, args []string) error { return syscall.Exec(path, args, os.Environ()) }, //nolint:gosec // runs the mse release it just installed
	}
	globalLogs := filepath.Join(installation.BasesFrom(os.Getenv, home).State, "mse")
	return runLogged(ctx, NewRootCommand(deps), os.Args[1:], globalLogs)
}

func bothTerminals() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) //nolint:gosec // file descriptors fit in an int
}

func wiringSteps(configarr wiring.OneOff, tool io.Writer) ([]wiring.Step, error) {
	docker, err := wiring.NewDocker()
	if err != nil {
		return nil, err
	}
	return wiring.Steps(docker, configarr, tool), nil
}

func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var errAlreadyReported = errors.New("already reported")

var endingSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

func interruptible() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), endingSignals...)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx, stop
}

func shieldSignals() (release func()) {
	ignored := make(chan os.Signal, 1)
	signal.Notify(ignored, endingSignals...)
	return func() { signal.Stop(ignored) }
}

func run(ctx context.Context, root *cobra.Command, args []string) int {
	return runLogged(ctx, root, args, "")
}

func runLogged(ctx context.Context, root *cobra.Command, args []string, globalLogs string) int {
	started := time.Now()
	log := logfile.New(logfile.Options{Limit: 10 << 20, Keep: 5, RunID: runIDFrom(args), Now: time.Now, Warn: root.ErrOrStderr()})
	defer log.Close()
	reporter := report.New(root.OutOrStdout(), root.ErrOrStderr(), log)
	root.SetOut(reporter.Stdout())
	root.SetErr(reporter.Stderr())
	root.SetArgs(args)
	if found, _, err := root.Find(args); err == nil {
		log.SetCommand(strings.TrimPrefix(found.CommandPath(), "mse "))
	}
	log.Line("", "start "+strings.Join(args, " "))
	code := 0
	if err := root.ExecuteContext(withLog(report.With(ctx, reporter), log)); err != nil {
		if !errors.Is(err, errAlreadyReported) {
			_, _ = fmt.Fprintln(root.ErrOrStderr(), paint.Stderr.Failure(fmt.Sprintf("mse: %v", err)))
		}
		code = 1
	}
	if warning := log.Line("", fmt.Sprintf("finish exit %d after %s", code, time.Since(started).Round(time.Millisecond))); warning != "" {
		_, _ = fmt.Fprintln(reporter.Stderr(), warning)
	}
	if !log.Opened() && globalLogs != "" {
		log.Open(globalLogs)
	}
	return code
}

var afterUpdateRunID = regexp.MustCompile(`^--after-update=([0-9a-f]{6})$`)

func runIDFrom(args []string) string {
	for _, arg := range args {
		if found := afterUpdateRunID.FindStringSubmatch(arg); found != nil {
			return found[1]
		}
	}
	return runID()
}

func runIDOf(ctx context.Context) string {
	if log, ok := ctx.Value(logKey{}).(*logfile.File); ok {
		return log.RunID()
	}
	return runID()
}

func runID() string {
	random := make([]byte, 3)
	_, _ = rand.Read(random)
	return hex.EncodeToString(random)
}

type logKey struct{}

func withLog(ctx context.Context, log *logfile.File) context.Context {
	return context.WithValue(ctx, logKey{}, log)
}

func openInstallationLog(ctx context.Context, state string) {
	if log, ok := ctx.Value(logKey{}).(*logfile.File); ok {
		log.Open(filepath.Join(state, "logs"))
	}
}
