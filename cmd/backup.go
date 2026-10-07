package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/backup"
	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/files"
	"github.com/pablovarela/media-server-engine/internal/healthchecks"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/restic"
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
}

var (
	backingUp = backupNeeds{stack: true, identity: true, excludes: true}
	restoring = backupNeeds{stack: true}
	telling   = backupNeeds{identity: true}
)

func newBackupCommands(deps Dependencies) []*cobra.Command {
	var takeOver, yes, overwrite bool
	backupCommand := func(use, short string, needs backupNeeds, do func(cmd *cobra.Command, b *backup.Backups) error) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := deps.backups(cmd, needs)
			if err != nil {
				return err
			}
			return do(cmd, b)
		}}
	}
	backupNow := backupCommand("backup", "Back up the apps' data now (stops them for a few minutes; only on the installation's main)", backingUp, func(cmd *cobra.Command, b *backup.Backups) error {
		if takeOver {
			return b.Claim(cmd.Context(), yes)
		}
		return b.Backup(cmd.Context())
	})
	backupNow.PreRunE = func(*cobra.Command, []string) error {
		if yes && !takeOver {
			return errors.New("--yes only goes with --take-over")
		}
		return nil
	}
	backupNow.Flags().BoolVar(&takeOver, "take-over", false, "make this machine the main, after asking when another machine is, then back up")
	backupNow.Flags().BoolVar(&yes, "yes", false, "with --take-over, take over without asking")
	restore := backupCommand("restore", "Restore volumes/ from the latest backup", restoring, func(cmd *cobra.Command, b *backup.Backups) error {
		return b.Restore(cmd.Context(), overwrite)
	})
	restore.Flags().BoolVar(&overwrite, "overwrite", false, "move the existing volumes/ aside and restore over it")
	return []*cobra.Command{
		backupNow,
		backupCommand("verify-backup", "Check the backups: restic check and a test restore of the latest snapshot's databases", telling, func(cmd *cobra.Command, b *backup.Backups) error {
			return b.Verify(cmd.Context())
		}),
		restore,
	}
}

func (d Dependencies) backups(cmd *cobra.Command, needs backupNeeds) (*backup.Backups, error) {
	binary, err := d.ResticBinary(cmd.Context())
	if err != nil {
		return nil, err
	}
	i, stack, err := d.backupTarget(cmd, needs.stack)
	if err != nil {
		return nil, err
	}
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
	return &backup.Backups{
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
	}, nil
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

func (d Dependencies) pinger(cmd *cobra.Command, i *installation.Installation) *healthchecks.Pings {
	short, role := d.shortHost(), i.Role()
	return &healthchecks.Pings{
		Client: d.HTTP, URL: healthchecks.PingURL, Key: healthchecksKey(i, "HEALTHCHECKS_PING_KEY"), Sleep: d.Sleep, ErrOut: cmd.ErrOrStderr(),
		Slug: func(job string) string { return healthchecks.Slug(i.Name, job, role, short) },
	}
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
