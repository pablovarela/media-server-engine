package join

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/pablovarela/media-server-engine/internal/backup"
)

var now = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func mainAt(hoursAgo float64) *backup.Main {
	return &backup.Main{Machine: "gorgon-pi", Time: now.Add(-time.Duration(hoursAgo * float64(time.Hour)))}
}

func TestDecide(t *testing.T) {
	tests := map[string]struct {
		current *backup.Main
		flags   Flags
		want    Decision
	}{
		"recent main": {current: mainAt(6), want: Decision{
			Say: "gorgon's main is gorgon-pi, last backup 2026-10-06 06:00.", Ask: true, Question: "Make this machine the main instead?", Default: Secondary}},
		"old main": {current: mainAt(72), want: Decision{
			Say: "gorgon's main is gorgon-pi, but its last backup was 2026-10-03 12:00; it looks gone.", Ask: true, Question: "Make this machine the main?", Default: Primary}},
		"no backups": {want: Decision{
			Say: "gorgon has no backups yet.", Ask: true, Question: "Make this machine the main?", Default: Primary}},
		"this machine": {current: &backup.Main{Machine: "pi", Time: now, ThisMachine: true}, want: Decision{
			Say: "This machine made gorgon's latest backup, so it is the main.", Role: Primary}},
		"--main":      {current: mainAt(6), flags: Flags{Main: true}, want: Decision{Role: Primary}},
		"--secondary": {current: mainAt(72), flags: Flags{Secondary: true}, want: Decision{Role: Secondary}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, Decide("gorgon", tt.current, now, tt.flags, false))
		})
	}
}

func TestDecideAtTheBoundary(t *testing.T) {
	assert.Equal(t, Secondary, Decide("gorgon", mainAt(48), now, Flags{}, false).Default)
	assert.Equal(t, Primary, Decide("gorgon", mainAt(48+1.0/60), now, Flags{}, false).Default)
}

func TestPlanData(t *testing.T) {
	tests := map[string]struct {
		current           *backup.Main
		held, restoreOver bool
		want              DataPlan
	}{
		"empty, with backups": {current: mainAt(6), want: DataPlan{Restore: true}},
		"held":                {current: mainAt(6), held: true, want: DataPlan{Say: "Kept the app data already in /d/volumes (--restore-over replaces it with the latest backup)."}},
		"held, restore over":  {current: mainAt(6), held: true, restoreOver: true, want: DataPlan{Restore: true, Overwrite: true}},
		"no backups":          {want: DataPlan{Say: "There are no backups yet, so the apps start empty."}},
		"no backups, held":    {held: true, want: DataPlan{Say: "Kept the app data already in /d/volumes; there are no backups yet."}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, PlanData("/d", tt.current, tt.held, tt.restoreOver))
		})
	}
}

func TestDecideKeepsTheMainThatMadeTheLatestBackup(t *testing.T) {
	this := &backup.Main{Machine: "pi", Time: now, ThisMachine: true}

	decision := Decide("gorgon", this, now, Flags{Secondary: true}, false)

	assert.Equal(t, Decision{Say: "This machine made gorgon's latest backup, so it stays the main (--secondary doesn't apply; another machine takes over with mse backup --take-over).", Role: Primary}, decision)
}

func TestDecideKeepsASecondaryThatKeptItsOwnData(t *testing.T) {
	kept := "Kept this machine a secondary: its app data wasn't restored, and gorgon-pi backed up at 2026-10-06 06:00, so backing this data up would make it gorgon's latest. " +
		"--restore-over restores the latest backup first; mse backup --take-over takes over later."
	assert.Equal(t, Decision{Say: kept, Role: Secondary}, Decide("gorgon", mainAt(6), now, Flags{}, true))
	assert.Equal(t, Decision{Role: Primary}, Decide("gorgon", mainAt(6), now, Flags{Main: true}, true), "--main is explicit")
	assert.True(t, Decide("gorgon", mainAt(72), now, Flags{}, true).Ask, "a main that looks gone can be taken over with the disk's data")
}
