package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
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
	"github.com/pablovarela/media-server-engine/internal/restic"
	"github.com/pablovarela/media-server-engine/internal/secrets"
)

type commandRunner interface {
	Run(ctx context.Context, c process.Command) (int, error)
	Output(ctx context.Context, c process.Command) (process.Result, error)
}

func newBackupCommands(deps Dependencies) []*cobra.Command {
	var yes, all, overwrite bool
	backupCommand := func(use, short string, withStack bool, do func(cmd *cobra.Command, b *backup.Backups) error) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := deps.backups(cmd, withStack)
			if err != nil {
				return err
			}
			return do(cmd, b)
		}}
	}
	claim := backupCommand("claim-backup-main", "Make this machine the installation's main, the one that backs up", true, func(cmd *cobra.Command, b *backup.Backups) error {
		return b.Claim(cmd.Context(), yes)
	})
	claim.Flags().BoolVar(&yes, "yes", false, "take over from another main without asking")
	unlock := backupCommand("unlock-backup", "Remove stale locks from the backup repository and show the ones left", false, func(cmd *cobra.Command, b *backup.Backups) error {
		return b.Unlock(cmd.Context(), all)
	})
	unlock.Flags().BoolVar(&all, "all", false, "remove every lock: only when no machine is running restic")
	restore := backupCommand("restore", "Restore volumes/ from the latest backup", true, func(cmd *cobra.Command, b *backup.Backups) error {
		return b.Restore(cmd.Context(), overwrite)
	})
	restore.Flags().BoolVar(&overwrite, "overwrite", false, "move the existing volumes/ aside and restore over it")
	return []*cobra.Command{
		backupCommand("backup", "Back up the apps' data now (stops them for a few minutes; only on the installation's main)", true, func(cmd *cobra.Command, b *backup.Backups) error {
			return b.Backup(cmd.Context())
		}),
		backupCommand("verify-backup", "Check the backups: restic check and a test restore of the latest snapshot's databases", false, func(cmd *cobra.Command, b *backup.Backups) error {
			return b.Verify(cmd.Context())
		}),
		backupCommand("backup-role", "Show which machine is the installation's main", false, func(cmd *cobra.Command, b *backup.Backups) error {
			return b.DescribeRole(cmd.Context())
		}),
		claim, unlock, restore,
	}
}

func (d Dependencies) backups(cmd *cobra.Command, withStack bool) (*backup.Backups, error) {
	i, stack, err := d.backupTarget(cmd, withStack)
	if err != nil {
		return nil, err
	}
	decrypted, err := d.Decrypt(filepath.Join(i.Config, "secrets", "backup.sops.env"))
	if err != nil {
		return nil, fmt.Errorf("decrypt backup.sops.env: %w", err)
	}
	repository := secrets.Dotenv(decrypted)
	hostname, err := d.Host.Hostname()
	if err != nil {
		return nil, err
	}
	short, _, _ := strings.Cut(hostname, ".")
	machine, err := backup.MachineID(d.MachineIDFile, i.Data)
	if err != nil {
		return nil, err
	}
	excludes, err := d.writeExcludes(i)
	if err != nil {
		return nil, err
	}
	role := i.Role()
	return &backup.Backups{
		Installation:       i,
		Repository:         resticFor(d.Run(cmd.OutOrStdout(), cmd.ErrOrStderr()), repository),
		RepositoryLocation: repository["RESTIC_REPOSITORY"],
		Stack:              stack,
		Pinger: &healthchecks.Pings{
			Client: d.HTTP, URL: healthchecks.PingURL, Key: pingKey(i), Sleep: d.Sleep, ErrOut: cmd.ErrOrStderr(),
			Slug: func(job string) string { return healthchecks.Slug(i.Name, job, role, short) },
		},
		MachineID:   machine,
		ShortHost:   short,
		ExcludeFile: excludes,
		TempDir:     tempDir(d.Environment),
		Now:         d.Now,
		Ask:         d.asker(cmd),
		Shield:      shieldSignals,
		Out:         cmd.OutOrStdout(),
		ErrOut:      cmd.ErrOrStderr(),
	}, nil
}

func (d Dependencies) backupTarget(cmd *cobra.Command, withStack bool) (*installation.Installation, backup.Stack, error) {
	if withStack {
		o, err := d.openProject(cmd, compose.Stack, noDrawing)
		if err != nil {
			return nil, nil, err
		}
		return o.installation, projectStack{runner: o.runner, project: o.project}, nil
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

func resticFor(runner commandRunner, repository map[string]string) restic.Restic {
	env := make([]string, 0, len(repository))
	for key, value := range repository {
		env = append(env, key+"="+value)
	}
	sort.Strings(env)
	return restic.Restic{Runner: runner, Env: env}
}

func pingKey(i *installation.Installation) string {
	text, _ := os.ReadFile(filepath.Join(i.State, ".secrets", "healthchecks.env")) //nolint:gosec // the installation's decrypted healthchecks keys
	return secrets.Dotenv(text)["HEALTHCHECKS_PING_KEY"]
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
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), question)
		line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		return strings.TrimSpace(line), true
	}
}

type projectStack struct {
	runner  composeRunner
	project *types.Project
}

func (s projectStack) RunningServices(ctx context.Context) ([]string, error) {
	return s.runner.RunningServices(ctx, s.project)
}

func (s projectStack) AnyRunning(ctx context.Context) (bool, error) {
	return s.runner.AnyRunning(ctx, s.project)
}

func (s projectStack) Stop(ctx context.Context) error {
	return s.runner.Stop(ctx, s.project)
}

func (s projectStack) Start(ctx context.Context, services []string) error {
	return s.runner.Start(ctx, s.project, services)
}
