package report

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type lines []string

func (l *lines) Line(tool, text string) {
	if tool != "" {
		text = tool + " | " + text
	}
	*l = append(*l, text)
}

func TestSteps(t *testing.T) {
	type Then struct {
		out, errOut string
		log         []string
	}
	tests := map[string]struct {
		When func(r *Reporter)
		Then Then
	}{
		"a step and its result on one line": {
			When: func(r *Reporter) { r.Step("Stopping the stack").Done("stopped 12 services") },
			Then: Then{out: "Stopping the stack... stopped 12 services.\n", log: []string{"Stopping the stack...", "Stopping the stack... stopped 12 services."}},
		},
		"tool output stays in the log": {
			When: func(r *Reporter) {
				s := r.Step("Backing up volumes/")
				_, _ = fmt.Fprintln(r.Tool("restic"), "repository 74dfde9e opened")
				s.Done("snapshot 40c4a929")
			},
			Then: Then{out: "Backing up volumes/... snapshot 40c4a929.\n", log: []string{"Backing up volumes/...", "restic | repository 74dfde9e opened", "Backing up volumes/... snapshot 40c4a929."}},
		},
		"a warning ends the step's line and the result follows indented": {
			When: func(r *Reporter) {
				s := r.Step("Drawing the landing page")
				r.Warn("could not read the checks")
				s.Done("done")
			},
			Then: Then{out: "Drawing the landing page...\n  done.\n", errOut: "could not read the checks\n",
				log: []string{"Drawing the landing page...", "could not read the checks", "Drawing the landing page... done."}},
		},
		"a failed step shows its last tool lines": {
			When: func(r *Reporter) {
				s := r.Step("Backing up volumes/")
				for n := 1; n <= 12; n++ {
					_, _ = fmt.Fprintf(r.Tool("restic"), "line %d\n", n)
				}
				_ = s.Fail(errors.New("restic backup failed (exit 3)"))
			},
			Then: Then{
				out:    "Backing up volumes/... failed.\n",
				errOut: "  line 3\n  line 4\n  line 5\n  line 6\n  line 7\n  line 8\n  line 9\n  line 10\n  line 11\n  line 12\n",
			},
		},
		"plain lines go through and are logged": {
			When: func(r *Reporter) {
				_, _ = fmt.Fprintln(r.Stdout(), "Backup done.")
				_, _ = fmt.Fprintln(r.Stderr(), "mse: boom")
			},
			Then: Then{out: "Backup done.\n", errOut: "mse: boom\n", log: []string{"Backup done.", "mse: boom"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			var log lines
			r := New(&out, &errOut, &log)

			tt.When(r)

			assert.Equal(t, tt.Then.out, out.String())
			assert.Equal(t, tt.Then.errOut, errOut.String())
			if tt.Then.log != nil {
				assert.Equal(t, tt.Then.log, []string(log))
			}
		})
	}
}

func TestVerboseShowsToolLines(t *testing.T) {
	var out bytes.Buffer
	r := New(&out, &bytes.Buffer{}, nil)
	r.SetVerbose(true)

	s := r.Step("Stopping the stack")
	_, _ = fmt.Fprint(r.Tool("compose"), "Container seerr Stopped\nContainer jellyfin Stopped\n")
	s.Done("stopped 2 services")

	assert.Equal(t, "Stopping the stack...\ncompose | Container seerr Stopped\ncompose | Container jellyfin Stopped\n  stopped 2 services.\n", out.String())
}

func TestStdoutPassesPartialLinesThrough(t *testing.T) {
	var errOut bytes.Buffer
	var log lines
	r := New(&bytes.Buffer{}, &errOut, &log)

	_, _ = fmt.Fprint(r.Stderr(), "Make this machine the main instead? (y/n) ")

	assert.Equal(t, "Make this machine the main instead? (y/n) ", errOut.String())
	_, _ = fmt.Fprint(r.Stderr(), "\n")
	assert.Equal(t, []string{"Make this machine the main instead? (y/n) "}, []string(log))
}

func TestFromAContextWithoutAReporter(t *testing.T) {
	assert.NotNil(t, From(t.Context()))
	assert.True(t, strings.HasPrefix(fmt.Sprintf("%T", From(t.Context())), "*report.Reporter"))
}

func TestDataIsShownButNotLogged(t *testing.T) {
	var out bytes.Buffer
	var log lines
	r := New(&out, &bytes.Buffer{}, &log)

	_, _ = fmt.Fprintln(r.Data(), "Jellyfin     pablo      secret")

	assert.Equal(t, "Jellyfin     pablo      secret\n", out.String())
	assert.Empty(t, log)
}
