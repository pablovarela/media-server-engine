package backup

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/restic"
)

func TestProgressShowsEveryMinuteWithWhatIsLeft(t *testing.T) {
	start := time.Date(2026, 10, 8, 0, 51, 0, 0, time.UTC)
	now := start
	var out bytes.Buffer
	b := &Backups{Now: func() time.Time { return now }, Report: report.New(&out, &out, nil)}
	step := b.Report.Step("Backing up the media")
	progress := b.progress(step)
	total := int64(45 * 1 << 30)

	for _, at := range []struct {
		after time.Duration
		done  float64
	}{
		{10 * time.Second, 0.01}, {59 * time.Second, 0.05}, {time.Minute, 0.25}, {90 * time.Second, 0.3}, {3 * time.Minute, 0.5}, {150 * time.Minute, 0.6},
	} {
		now = start.Add(at.after)
		progress(restic.Progress{Done: at.done, TotalBytes: total, BytesDone: int64(at.done * float64(total))})
	}

	assert.Equal(t, "Backing up the media...\n"+
		"  25% of 45.0 GiB, about 3 min left (done around 00:55)\n"+
		"  50% of 45.0 GiB, about 3 min left (done around 00:57)\n"+
		"  60% of 45.0 GiB, about 1 h 40 min left (done around 05:01)\n", out.String())
}

func TestTimeLeft(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 51, 0, 0, time.UTC)
	tests := map[time.Duration]string{
		40 * time.Second:             "about 1 min left (done around 00:51)",
		48 * time.Minute:             "about 48 min left (done around 01:39)",
		3*time.Hour + 20*time.Minute: "about 3 h 20 min left (done around 04:11)",
		2*24*time.Hour + 4*time.Hour: "about 2 days 4 h left (done around Sat 04:51)",
	}
	for left, want := range tests {
		assert.Equal(t, want, timeLeft(left, now), left)
	}
}
