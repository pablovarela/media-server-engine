package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/backup"
	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/files"
	"github.com/pablovarela/media-server-engine/internal/healthchecks"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/media"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/restic"
	"github.com/pablovarela/media-server-engine/internal/runs"
	"github.com/pablovarela/media-server-engine/internal/secrets"
)

type commandRunner interface {
	Run(ctx context.Context, c process.Command) (int, error)
	Output(ctx context.Context, c process.Command) (process.Result, error)
}

type backupNeeds struct {
	stack    bool
	identity bool
	excludes bool
	media    bool
}

var (
	backingUp = backupNeeds{stack: true, identity: true, excludes: true}
	telling   = backupNeeds{identity: true}
)

func newBackupCommands(deps Dependencies) []*cobra.Command {
	var f backupFlags
	backupCommand := func(use, short string, needs backupNeeds, do func(cmd *cobra.Command, b *backup.Backups) error) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := deps.backups(cmd, needs)
			if err != nil {
				return err
			}
			return do(cmd, b)
		}}
	}
	backupNow := &cobra.Command{
		Use: "backup", Short: "Back up the installation now: --apps stops the apps for a few minutes, --media leaves them running (only on the main)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := deps.backups(cmd, backupNeeds{stack: f.apps, identity: true, excludes: f.apps, media: f.media})
			if err != nil {
				return err
			}
			return f.backUp(cmd.Context(), b)
		},
	}
	backupNow.PreRunE = func(*cobra.Command, []string) error { return f.checkBackup() }
	backupNow.Flags().BoolVar(&f.apps, "apps", false, "back up the apps' data: their databases and settings under volumes/")
	backupNow.Flags().BoolVar(&f.media, "media", false, "back up data/media to the media backup repository; the apps keep running")
	backupNow.Flags().BoolVar(&f.takeOver, "take-over", false, "with --apps, make this machine the main, after asking when another machine is, then back up")
	backupNow.Flags().BoolVar(&f.yes, "yes", false, "with --apps --take-over, take over without asking")
	restore := &cobra.Command{
		Use: "restore", Short: "Restore from the latest backup: --apps restores volumes/, --media restores data/media and starts the stack", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := deps.backups(cmd, backupNeeds{stack: true, media: f.media})
			if err != nil {
				return err
			}
			if err := f.restore(cmd.Context(), b); err != nil || !f.media {
				return err
			}
			return deps.apply(cmd)
		},
	}
	restore.PreRunE = func(*cobra.Command, []string) error { return f.checkRestore() }
	restore.Flags().BoolVar(&f.apps, "apps", false, "restore volumes/ from the latest apps backup")
	restore.Flags().BoolVar(&f.media, "media", false, "restore data/media from the latest media backup, keeping files that match, then start the stack")
	restore.Flags().BoolVar(&f.overwrite, "overwrite", false, "with --apps, move the existing volumes/ aside and restore over it")
	return []*cobra.Command{
		backupNow,
		backupCommand("check-backup", "Check the backups: restic check and a test restore of the latest snapshot's databases", telling, func(cmd *cobra.Command, b *backup.Backups) error {
			return b.Verify(cmd.Context())
		}),
		restore,
	}
}

type backupFlags struct {
	apps, media, takeOver, yes, overwrite bool
}

func (f *backupFlags) backUp(ctx context.Context, b *backup.Backups) error {
	if f.apps {
		backUpApps := b.Backup
		if f.takeOver {
			backUpApps = func(ctx context.Context) error { return b.Claim(ctx, f.yes) }
		}
		if err := backUpApps(ctx); err != nil {
			return err
		}
	}
	if f.media {
		return b.BackupMedia(ctx)
	}
	return nil
}

func (f *backupFlags) restore(ctx context.Context, b *backup.Backups) error {
	if f.apps {
		if err := b.Restore(ctx, f.overwrite); err != nil {
			return err
		}
	}
	if f.media {
		return b.RestoreMedia(ctx)
	}
	return nil
}

func (f *backupFlags) checkBackup() error {
	switch {
	case f.takeOver && !f.apps:
		return errors.New("--take-over only goes with --apps")
	case !f.apps && !f.media:
		return errors.New("say what to back up: --apps, --media, or both")
	case f.yes && !f.takeOver:
		return errors.New("--yes only goes with --take-over")
	}
	return nil
}

func (f *backupFlags) checkRestore() error {
	switch {
	case f.overwrite && !f.apps:
		return errors.New("--overwrite only goes with --apps")
	case !f.apps && !f.media:
		return errors.New("say what to restore: --apps, --media, or both")
	}
	return nil
}

func (d Dependencies) backups(cmd *cobra.Command, needs backupNeeds) (*backup.Backups, error) {
	if needs.media {
		if err := d.mediaBackupOn(cmd); err != nil {
			return nil, err
		}
	}
	binary, err := d.ResticBinary(cmd.Context())
	if err != nil {
		return nil, err
	}
	i, stack, err := d.backupTarget(cmd, needs.stack)
	if err != nil {
		return nil, err
	}
	repository, err := d.backupCredentials(i)
	if err != nil {
		return nil, err
	}
	hostname, err := d.Host.Hostname()
	if err != nil {
		return nil, err
	}
	short, _, _ := strings.Cut(hostname, ".")
	machine, excludes, err := d.backupFiles(i, needs)
	if err != nil {
		return nil, err
	}
	tool := report.From(cmd.Context()).Tool("restic")
	b := &backup.Backups{
		Installation:       i,
		Repository:         resticFor(binary, d.Run(tool, tool), repository, tool),
		RepositoryLocation: repository["RESTIC_REPOSITORY"],
		Stack:              stack,
		Pinger:             d.pinger(cmd, i),
		MachineID:          machine,
		ShortHost:          short,
		ExcludeFile:        excludes,
		TempDir:            tempDir(d.Environment),
		Now:                d.Now,
		Ask:                d.asker(cmd),
		Shield:             shieldSignals,
		Report:             report.From(cmd.Context()),
		FreeSpace:          backup.FreeSpace,
	}
	if needs.media {
		if b.MediaRepository, b.Media, err = d.mediaRepository(i, repository, binary, tool); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func (d Dependencies) backupCredentials(i *installation.Installation) (map[string]string, error) {
	decrypted, err := d.Decrypt(filepath.Join(i.Config, "secrets", "backup.sops.env"))
	if err != nil {
		return nil, fmt.Errorf("decrypt backup.sops.env: %w", err)
	}
	repository := secrets.Dotenv(decrypted)
	if repository["RESTIC_REPOSITORY"] == "" {
		repository["RESTIC_REPOSITORY"] = i.Settings["RESTIC_REPOSITORY"]
	}
	if repository["RESTIC_REPOSITORY"] == "" {
		return nil, fmt.Errorf("%s has no backup repository: set RESTIC_REPOSITORY in installation.env", i.Name)
	}
	return repository, nil
}

func (d Dependencies) mediaBackupOn(cmd *cobra.Command) error {
	i, err := d.installation(cmd)
	if err != nil {
		return err
	}
	timing, err := media.Timing(i.Settings)
	if err == nil && !timing.Enabled {
		return fmt.Errorf("%s has no media backup; turn it on with mse configure", i.Name)
	}
	return err
}

func (d Dependencies) mediaRepository(i *installation.Installation, credentials map[string]string, binary string, tool io.Writer) (backup.Repository, media.Settings, error) {
	settings := maps.Clone(i.Settings)
	settings["RESTIC_REPOSITORY"] = credentials["RESTIC_REPOSITORY"]
	m, err := media.From(settings)
	if err != nil {
		return nil, media.Settings{}, err
	}
	if !m.Enabled {
		return nil, media.Settings{}, fmt.Errorf("%s has no media backup; turn it on with mse configure", i.Name)
	}
	environment := maps.Clone(credentials)
	environment["RESTIC_REPOSITORY"] = m.Repository
	return resticFor(binary, d.Run(tool, tool), environment, tool), m, nil
}

func (d Dependencies) backupFiles(i *installation.Installation, needs backupNeeds) (machine, excludes string, err error) {
	if needs.identity {
		if machine, err = backup.MachineID(d.MachineIDFile, i.Data); err != nil {
			return "", "", err
		}
	}
	if needs.excludes {
		if excludes, err = d.writeExcludes(i); err != nil {
			return "", "", err
		}
	}
	return machine, excludes, nil
}

func (d Dependencies) backupTarget(cmd *cobra.Command, withStack bool) (*installation.Installation, backup.Stack, error) {
	if withStack {
		o, err := d.openProject(cmd, compose.Stack, noDrawing)
		if err != nil {
			return nil, nil, err
		}
		return o.installation, projectStack{runner: o.runner, project: o.project, outcomes: o.outcomes}, nil
	}
	i, err := d.installation(cmd)
	if err != nil {
		return nil, nil, err
	}
	return i, nil, secrets.WriteAll(i, d.Decrypt, secrets.RandomKey)
}

func (d Dependencies) writeExcludes(i *installation.Installation) (string, error) {
	content, err := fs.ReadFile(d.Engine, "scripts/backup-excludes.txt")
	if err != nil {
		return "", err
	}
	path := filepath.Join(i.State, "backup-excludes.txt")
	_, err = files.WriteIfChanged(path, content, 0o644)
	return path, err
}

func resticFor(binary string, runner commandRunner, repository map[string]string, tool io.Writer) restic.Restic {
	env := make([]string, 0, len(repository))
	for key, value := range repository {
		env = append(env, key+"="+value)
	}
	sort.Strings(env)
	return restic.Restic{Binary: binary, Runner: runner, Env: env, Log: tool}
}

func (d Dependencies) pinger(cmd *cobra.Command, i *installation.Installation) recordedPings {
	short, role := d.shortHost(), i.Role()
	return recordedPings{
		pings: &healthchecks.Pings{
			Client: d.HTTP, URL: healthchecks.PingURL, Key: healthchecksKey(i, "HEALTHCHECKS_PING_KEY"), Sleep: d.Sleep, ErrOut: cmd.ErrOrStderr(),
			Slug: func(job string) string { return healthchecks.Slug(i.Name, job, role, short) },
		},
		runs: d.recorder(cmd, i),
	}
}

type recordedPings struct {
	pings *healthchecks.Pings
	runs  runRecorder
}

func (p recordedPings) Ping(ctx context.Context, job, suffix string) {
	switch suffix {
	case "/start":
		p.runs.start(job)
		if !timedByHealthchecks(job) {
			return
		}
	case "":
		p.runs.finish(job, false)
	case "/fail":
		p.runs.finish(job, true)
	}
	p.pings.Ping(ctx, job, suffix)
}

// A first media upload can take days; with a start ping healthchecks.io would call it down once it outlasts the grace.
func timedByHealthchecks(job string) bool {
	return job != backup.MediaJob
}

type runRecorder struct {
	runs   runs.Recorder
	errOut io.Writer
}

func (r runRecorder) start(job string) { r.warn(job, r.runs.Start(job)) }

func (r runRecorder) finish(job string, failed bool) { r.warn(job, r.runs.Finish(job, failed)) }

func (r runRecorder) warn(job string, err error) {
	if err != nil {
		_, _ = fmt.Fprintf(r.errOut, "could not record the %s run for mse status: %v\n", job, err)
	}
}

func (d Dependencies) recorder(cmd *cobra.Command, i *installation.Installation) runRecorder {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return runRecorder{runs: runs.Recorder{Dir: runsDir(i), Now: now, PID: os.Getpid(), Boot: runs.BootID()}, errOut: cmd.ErrOrStderr()}
}

func runsDir(i *installation.Installation) string {
	return filepath.Join(i.State, "runs")
}

func (d Dependencies) shortHost() string {
	hostname, _ := d.Host.Hostname()
	short, _, _ := strings.Cut(hostname, ".")
	return short
}

func healthchecksKey(i *installation.Installation, name string) string {
	return secrets.Dotenv(readSecretsFile(i, "healthchecks.env"))[name]
}

func tempDir(getenv func(string) string) string {
	if dir := getenv("TMPDIR"); dir != "" {
		return dir
	}
	return "/var/tmp"
}

func (d Dependencies) asker(cmd *cobra.Command) func(string) (string, bool) {
	return func(question string) (string, bool) {
		if d.Interactive == nil || !d.Interactive() {
			return "", false
		}
		r := report.From(cmd.Context())
		r.Prompt(question)
		line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		answer := strings.TrimSpace(line)
		r.Note(question + answer)
		return answer, true
	}
}

type projectStack struct {
	runner   composeRunner
	project  *types.Project
	outcomes *compose.Outcomes
}

func (s projectStack) RunningServices(ctx context.Context) ([]string, error) {
	return s.runner.RunningServices(ctx, s.project)
}

func (s projectStack) AnyRunning(ctx context.Context) (bool, error) {
	return s.runner.AnyRunning(ctx, s.project)
}

func (s projectStack) Stop(ctx context.Context) (string, error) {
	err := s.runner.Stop(ctx, s.project)
	return s.outcomes.Take().String(), err
}

func (s projectStack) Start(ctx context.Context, services []string) (string, error) {
	err := s.runner.Start(ctx, s.project, services)
	return s.outcomes.Take().String(), err
}
