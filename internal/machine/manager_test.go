package machine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/process"
)

func TestManagerWithoutDocker(t *testing.T) {
	tests := map[string]struct {
		groups, manager string
		uid             string
		stale           bool
	}{
		"started before joining": {groups: "pablo docker", manager: "4 1000", uid: "1000", stale: true},
		"has the group":          {groups: "pablo docker", manager: "4 995 1000", uid: "1000"},
		"account not in docker":  {groups: "pablo adm", manager: "4 1000"},
		"manager unreadable":     {groups: "pablo docker"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.answers["id -Gn pablo"] = process.Result{Stdout: []byte(tt.groups + "\n")}
			status := filepath.Join(f.env.ProcRoot, "4242", "status")
			if tt.manager == "" {
				require.NoError(t, os.Remove(status))
			} else {
				require.NoError(t, os.WriteFile(status, []byte("Groups:\t"+tt.manager+"\n"), 0o644))
			}

			uid, stale := ManagerWithoutDocker(context.Background(), f.env.Runner, f.env.ProcRoot, "pablo")

			assert.Equal(t, tt.stale, stale)
			if tt.stale {
				assert.Equal(t, tt.uid, uid)
			}
		})
	}
}
