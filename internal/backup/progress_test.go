package backup

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/restic"
)

type progressAt struct {
	after time.Duration
	done  float64
}

func progressLines(t *testing.T, points []progressAt) string {
	t.Helper()
	start := time.Date(2026, 10, 8, 0, 51, 0, 0, time.UTC)
	now := start
	var out bytes.Buffer
	b := &Backups{Now: func() time.Time { return now }, Report: report.New(&out, &out, nil)}
	progress := b.progress(b.Report.Step("Backing up the media"))
	total := int64(45 * 1 << 30)
	for _, at := range points {
		now = start.Add(at.after)
		progress(restic.Progress{Done: at.done, TotalBytes: total, BytesDone: int64(at.done * float64(total))})
	}
	return out.String()
}

func TestProgress(t *testing.T) {
	steady := []progressAt{{time.Minute, 0.16}}
	for minute := 2; minute <= 8; minute++ {
		steady = append(steady, progressAt{time.Duration(minute) * time.Minute, 0.16 + 0.02*float64(minute-1)})
	}
	type Given struct {
		points []progressAt
	}
	type Then struct {
		lines []string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"at most one line a minute": {
			Given: Given{points: []progressAt{{10 * time.Second, 0.01}, {59 * time.Second, 0.05}, {time.Minute, 0.25}, {90 * time.Second, 0.3}, {2 * time.Minute, 0.5}}},
			Then: Then{lines: []string{
				"25% of 45.0 GiB, about 3 min left (done around 00:55)",
				"50% of 45.0 GiB, about 2 min left (done around 00:55)",
			}},
		},
		"after a fast start, only the last five minutes count": {
			Given: Given{points: steady},
			Then: Then{lines: []string{
				"16% of 45.0 GiB, about 5 min left (done around 00:57)",
				"28% of 45.0 GiB, about 36 min left (done around 01:34)",
				"30% of 45.0 GiB, about 35 min left (done around 01:34)",
			}},
		},
		"after a long silence, the speed since the last line": {
			Given: Given{points: []progressAt{{time.Minute, 0.1}, {11 * time.Minute, 0.2}}},
			Then:  Then{lines: []string{"20% of 45.0 GiB, about 1 h 20 min left (done around 02:22)"}},
		},
		"no progress over five minutes gives no time left": {
			Given: Given{points: []progressAt{{time.Minute, 0.1}, {2 * time.Minute, 0.1}, {7 * time.Minute, 0.1}}},
			Then:  Then{lines: []string{"10% of 45.0 GiB\n"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			out := progressLines(t, tt.Given.points)

			for _, line := range tt.Then.lines {
				assert.Contains(t, out, "  "+line)
			}
		})
	}
}

func TestTimeLeft(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 51, 0, 0, time.UTC)
	tests := map[time.Duration]string{
		40 * time.Second:             "about 1 min left (done around 00:52)",
		48 * time.Minute:             "about 48 min left (done around 01:39)",
		3*time.Hour + 20*time.Minute: "about 3 h 20 min left (done around 04:11)",
		2*24*time.Hour + 4*time.Hour: "about 2 days 4 h left (done around Sat 04:51)",
	}
	for left, want := range tests {
		assert.Equal(t, want, timeLeft(left, now), left)
	}
}
