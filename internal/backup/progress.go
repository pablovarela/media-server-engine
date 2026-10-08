package backup

import (
	"fmt"
	"time"

	"github.com/pablovarela/media-server-engine/internal/report"
	"github.com/pablovarela/media-server-engine/internal/restic"
)

const (
	progressEvery = time.Minute
	speedOver     = 5 * time.Minute
)

type progressSample struct {
	at   time.Time
	done float64
}

func (b *Backups) progress(step *report.Step) func(restic.Progress) {
	started := b.Now()
	shown := started
	samples := []progressSample{{at: started}}
	return func(p restic.Progress) {
		now := b.Now()
		if now.Sub(shown) < progressEvery || p.Done <= 0 {
			return
		}
		shown = now
		samples = sinceSpeedWindow(samples, now)
		since := samples[0]
		samples = append(samples, progressSample{at: now, done: p.Done})
		line := fmt.Sprintf("%d%% of %s", int(p.Done*100), restic.Size(p.TotalBytes))
		if gained := p.Done - since.done; gained > 0 {
			left := time.Duration(float64(now.Sub(since.at)) * (1 - p.Done) / gained)
			line += ", " + timeLeft(left, now)
		}
		step.Progress(line)
	}
}

func sinceSpeedWindow(samples []progressSample, now time.Time) []progressSample {
	for len(samples) > 1 && now.Sub(samples[1].at) >= speedOver {
		samples = samples[1:]
	}
	return samples
}

func timeLeft(left time.Duration, now time.Time) string {
	left = max(left.Round(time.Minute), time.Minute)
	finish := now.Add(left).Format("15:04")
	if left >= 20*time.Hour {
		finish = now.Add(left).Format("Mon 15:04")
	}
	return fmt.Sprintf("about %s left (done around %s)", inWords(int(left.Minutes())), finish)
}

func inWords(minutes int) string {
	switch {
	case minutes < 60:
		return fmt.Sprintf("%d min", minutes)
	case minutes < 24*60:
		return fmt.Sprintf("%d h %d min", minutes/60, minutes%60)
	}
	return fmt.Sprintf("%d days %d h", minutes/(24*60), minutes%(24*60)/60)
}
