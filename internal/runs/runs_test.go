package runs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	started = time.Date(2026, 10, 7, 4, 30, 0, 0, time.UTC)
	ended   = started.Add(time.Minute)
)

func clock(times ...time.Time) func() time.Time {
	return func() time.Time {
		now := times[0]
		times = times[1:]
		return now
	}
}

func TestRecorder(t *testing.T) {
	type Given struct {
		earlier string
	}
	type When struct {
		record func(r Recorder) error
	}
	type Then struct {
		run      Run
		recorded bool
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"nothing recorded yet": {
			When: When{record: func(Recorder) error { return nil }},
		},
		"started": {
			When: When{record: func(r Recorder) error { return r.Start("backup") }},
			Then: Then{recorded: true, run: Run{Started: started, PID: 4242, Boot: "boot-1"}},
		},
		"succeeded": {
			When: When{record: func(r Recorder) error {
				if err := r.Start("backup"); err != nil {
					return err
				}
				return r.Finish("backup", false)
			}},
			Then: Then{recorded: true, run: Run{Started: started, PID: 4242, Boot: "boot-1", Ended: ended}},
		},
		"failed": {
			When: When{record: func(r Recorder) error {
				if err := r.Start("backup"); err != nil {
					return err
				}
				return r.Finish("backup", true)
			}},
			Then: Then{recorded: true, run: Run{Started: started, PID: 4242, Boot: "boot-1", Ended: ended, Failed: true}},
		},
		"failed before it started": {
			When: When{record: func(r Recorder) error { return r.Finish("backup", true) }},
			Then: Then{recorded: true, run: Run{Started: started, PID: 4242, Boot: "boot-1", Ended: started, Failed: true}},
		},
		"another process's unfinished run is not closed by this one": {
			Given: Given{earlier: `{"started":"2026-10-07T01:00:00Z","pid":1111,"boot":"boot-1"}`},
			When:  When{record: func(r Recorder) error { return r.Finish("backup", false) }},
			Then:  Then{recorded: true, run: Run{Started: started, PID: 4242, Boot: "boot-1", Ended: started}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "runs")
			if tt.Given.earlier != "" {
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "backup.json"), []byte(tt.Given.earlier), 0o644))
			}
			r := Recorder{Dir: dir, Now: clock(started, ended), PID: 4242, Boot: "boot-1"}

			require.NoError(t, tt.When.record(r))

			run, recorded, err := Read(dir, "backup")
			require.NoError(t, err)
			assert.Equal(t, tt.Then.recorded, recorded)
			assert.True(t, tt.Then.run.Started.Equal(run.Started) && tt.Then.run.Ended.Equal(run.Ended), "%+v", run)
			assert.Equal(t, tt.Then.run.PID, run.PID)
			assert.Equal(t, tt.Then.run.Boot, run.Boot)
			assert.Equal(t, tt.Then.run.Failed, run.Failed)
		})
	}
}

func TestAnUnreadableRecordIsAnError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "backup.json"), nil, 0o644))

	_, _, err := Read(dir, "backup")

	assert.ErrorContains(t, err, "backup.json")
}

func TestRunState(t *testing.T) {
	alive := func(run Run) bool { return run.PID == 4242 }
	tests := map[string]struct {
		Given Run
		Then  State
	}{
		"never ran":                      {Given: Run{}, Then: NeverRan},
		"ended well":                     {Given: Run{Started: started, PID: 1, Ended: ended}, Then: Succeeded},
		"ended badly":                    {Given: Run{Started: started, PID: 1, Ended: ended, Failed: true}, Then: Failed},
		"no end, its process still runs": {Given: Run{Started: started, PID: 4242}, Then: Running},
		"no end, its process is gone":    {Given: Run{Started: started, PID: 1}, Then: Interrupted},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.Then, tt.Given.State(alive))
		})
	}
}

func TestAlive(t *testing.T) {
	this := os.Getpid()
	tests := map[string]struct {
		Given Run
		Then  bool
	}{
		"this process, this boot":       {Given: Run{PID: this, Boot: "boot-1"}, Then: true},
		"this process ID, another boot": {Given: Run{PID: this, Boot: "boot-0"}, Then: false},
		"a process that doesn't exist":  {Given: Run{PID: 1 << 30, Boot: "boot-1"}, Then: false},
		"no process":                    {Given: Run{Boot: "boot-1"}, Then: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.Then, Alive("boot-1")(tt.Given))
		})
	}
}
