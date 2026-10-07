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

const (
	recentBackup = 48 * time.Hour
	timeLayout   = "2006-01-02 15:04"
)

type Decision struct {
	Say      string
	Ask      bool
	Question string
	Default  Role
	Role     Role
}

func Decide(name string, current *backup.Main, now time.Time, kept bool) Decision {
	if decision, settled := settled(name, current, now, kept); settled {
		return decision
	}
	return question(name, current, now)
}

func settled(name string, current *backup.Main, now time.Time, kept bool) (Decision, bool) {
	switch {
	case kept && recent(current, now):
		return Decision{Say: fmt.Sprintf("Kept this machine a copy that doesn't back up: its app data wasn't restored, and %s backed up at %s, so backing this data up would make it %s's latest. "+
			"mse restore --overwrite restores the latest backup; mse backup --take-over takes over later.", current.Machine, current.Time.Format(timeLayout), name), Role: Secondary}, true
	case current != nil && current.ThisMachine:
		return Decision{Say: "This machine made " + name + "'s latest backup, so it is the main.", Role: Primary}, true
	}
	return Decision{}, false
}

func question(name string, current *backup.Main, now time.Time) Decision {
	switch {
	case current == nil:
		return Decision{Say: name + " has no backups yet.", Ask: true, Question: "Make this machine the main?", Default: Primary}
	case recent(current, now):
		return Decision{Say: fmt.Sprintf("%s's main is %s, last backup %s.", name, current.Machine, current.Time.Format(timeLayout)),
			Ask: true, Question: "Make this machine the main instead?", Default: Secondary}
	}
	return Decision{Say: fmt.Sprintf("%s's main is %s, but its last backup was %s; it looks gone.", name, current.Machine, current.Time.Format(timeLayout)),
		Ask: true, Question: "Make this machine the main?", Default: Primary}
}

func recent(current *backup.Main, now time.Time) bool {
	return current != nil && !current.ThisMachine && now.Sub(current.Time) <= recentBackup
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
		return DataPlan{Say: "Kept the app data already in " + volumes + " (mse restore --overwrite replaces it with the latest backup)."}
	}
	return DataPlan{Restore: true, Overwrite: held}
}
