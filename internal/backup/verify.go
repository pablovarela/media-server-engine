package backup

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/pablovarela/media-server-engine/internal/paint"
	"github.com/pablovarela/media-server-engine/internal/restic"
)

var databaseSuffixes = []string{".db", ".sqlite", ".sqlite3"}

func (b *Backups) Verify(ctx context.Context) (err error) {
	finishing := context.WithoutCancel(ctx)
	defer func() {
		if err != nil {
			b.Pinger.Ping(finishing, "verify", "/fail")
		}
	}()
	b.Pinger.Ping(ctx, "verify", "/start")
	state, latest, err := b.main(ctx)
	if err != nil {
		return fmt.Errorf("%w; nothing was checked", err)
	}
	if state == anotherMachine {
		return fmt.Errorf("another machine is %s's main; its verification runs there", b.Installation.Name)
	}
	if err := b.Repository.Unlock(ctx); err != nil {
		return err
	}
	b.say("Checking the repository...")
	if err := b.Repository.Check(ctx); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(b.TempDir, "mse-verify.")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	b.say("Restoring the databases of the latest snapshot...")
	host := ""
	if latest != nil {
		host = b.Installation.Name
	}
	if err := b.Repository.Restore(ctx, restic.RestoreOptions{Snapshot: "latest", Host: host, Target: dir, Include: []string{"*.db", "*.sqlite", "*.sqlite3"}}); err != nil {
		return err
	}
	if err := b.checkDatabases(ctx, dir); err != nil {
		return err
	}
	b.Pinger.Ping(ctx, "verify", "")
	b.say(paint.Stdout.Success("The backups check out."))
	return nil
}

func (b *Backups) checkDatabases(ctx context.Context, dir string) error {
	found, err := databases(dir)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return errors.New("the latest snapshot holds no databases")
	}
	b.say(fmt.Sprintf("Checking %d databases...", len(found)))
	for _, path := range found {
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if err := checkDatabase(ctx, path, relative); err != nil {
			return err
		}
	}
	return nil
}

func databases(dir string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && hasDatabaseSuffix(entry.Name()) {
			found = append(found, path)
		}
		return nil
	})
	return found, err
}

func hasDatabaseSuffix(name string) bool {
	for _, suffix := range databaseSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func isSQLite(path string) bool {
	file, err := os.Open(path) //nolint:gosec // a file restic restored into the verify folder
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	header := make([]byte, 16)
	n, _ := io.ReadFull(file, header)
	return bytes.HasPrefix(header[:n], []byte("SQLite format 3"))
}

func checkDatabase(ctx context.Context, path, name string) error {
	if !isSQLite(path) {
		return nil
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("cannot check %s: %w", name, err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("cannot check %s: %w", name, err)
	}
	defer func() { _ = rows.Close() }()
	var results []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return fmt.Errorf("cannot check %s: %w", name, err)
		}
		results = append(results, line)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("cannot check %s: %w", name, err)
	}
	if result := strings.Join(results, "\n"); result != "ok" {
		return fmt.Errorf("integrity check failed for %s: %s", name, result)
	}
	return nil
}
