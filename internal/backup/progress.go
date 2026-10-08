package backup

import (
	"fmt"
	"time"

	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/restic"
)

const progressEvery = time.Minute

func (b *Backups) progress(step *report.Step) func(restic.Progress) {
	started := b.Now()
	shown := started
	return func(p restic.Progress) {
		now := b.Now()
		if now.Sub(shown) < progressEvery || p.Done <= 0 {
			return
		}
		shown = now
		elapsed := now.Sub(started)
		left := time.Duration(float64(elapsed) * (1 - p.Done) / p.Done)
		step.Progress(fmt.Sprintf("%d%% of %s, %s", int(p.Done*100), restic.Size(p.TotalBytes), timeLeft(left, now)))
	}
}

func timeLeft(left time.Duration, now time.Time) string {
	finish := now.Add(left).Format("15:04")
	if left >= 20*time.Hour {
		finish = now.Add(left).Format("Mon 15:04")
	}
	return fmt.Sprintf("about %s left (done around %s)", roughly(left), finish)
}

func roughly(d time.Duration) string {
	minutes := int(d.Round(time.Minute).Minutes())
	switch {
	case minutes < 1:
		return "1 min"
	case minutes < 60:
		return fmt.Sprintf("%d min", minutes)
	case minutes < 24*60:
		return fmt.Sprintf("%d h %d min", minutes/60, minutes%60)
	}
	return fmt.Sprintf("%d days %d h", minutes/(24*60), minutes%(24*60)/60)
}
