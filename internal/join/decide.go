package join

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/pablovarela/media-server-engine/internal/backup"
)

type Role int

const (
	Secondary Role = iota
	Primary
)

const recentBackup = 48 * time.Hour

type Flags struct{ Main, Secondary bool }

type Decision struct {
	Say      string
	Ask      bool
	Question string
	Default  Role
	Role     Role
}

func Decide(name string, current *backup.Main, now time.Time, flags Flags) Decision {
	switch {
	case flags.Main:
		return Decision{Role: Primary}
	case flags.Secondary:
		return Decision{Role: Secondary}
	case current == nil:
		return Decision{Say: name + " has no backups yet.", Ask: true, Question: "Make this machine the main?", Default: Primary}
	case current.ThisMachine:
		return Decision{Say: "This machine made " + name + "'s latest backup, so it is the main.", Role: Primary}
	case now.Sub(current.Time) <= recentBackup:
		return Decision{Say: fmt.Sprintf("%s's main is %s, last backup %s.", name, current.Machine, current.Time.Format("2006-01-02 15:04")),
			Ask: true, Question: "Make this machine the main instead?", Default: Secondary}
	}
	return Decision{Say: fmt.Sprintf("%s's main is %s, but its last backup was %s; it looks gone.", name, current.Machine, current.Time.Format("2006-01-02 15:04")),
		Ask: true, Question: "Make this machine the main?", Default: Primary}
}

type DataPlan struct {
	Restore, Overwrite bool
	Say                string
}

func PlanData(data string, current *backup.Main, held, restoreOver bool) DataPlan {
	volumes := filepath.Join(data, "volumes")
	switch {
	case current == nil && held:
		return DataPlan{Say: "Kept the app data already in " + volumes + "; there are no backups yet."}
	case current == nil:
		return DataPlan{Say: "There are no backups yet, so the apps start empty."}
	case held && !restoreOver:
		return DataPlan{Say: "Kept the app data already in " + volumes + " (--restore-over replaces it with the latest backup)."}
	}
	return DataPlan{Restore: true, Overwrite: held}
}
