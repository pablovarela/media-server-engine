package wiring

import (
	"context"
	"crypto/sha1" //nolint:gosec // Deluge's own password hash
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	pluginSource = "b4af725398ecf4586cdb5d15c8637465de2d7614.tar.gz"
	pluginSHA256 = "c400969cff22d7be00cbd7c55487380ad876f0c2a814b206866ef9c1ae62dedb"
	delugeCore   = `{"enabled_plugins": [], "max_upload_speed": -1.0, "download_location": "/downloads"}`
	loggedIn     = `{"result": true, "error": null, "id": 1}`
)

type delugeFixture struct {
	t      *testing.T
	env    Env
	out    *said
	r      *routes
	docker *mockDocker
	config string
	calls  []string
}

func newDelugeFixture(t *testing.T, password string) *delugeFixture {
	t.Helper()
	f := &delugeFixture{t: t, r: newRoutes(t).on("POST", "/json", ok(loggedIn)), docker: newMockDocker(t)}
	f.env, f.out = testEnv(t, f.r.client(), map[string]string{"DELUGE_WEB_PASSWORD": password})
	f.env.Settings["DELUGE_URL"] = "http://deluge"
	template, err := os.ReadFile(filepath.Join("..", "..", "config-template", "apps.yml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(f.env.Config, "apps.yml"), template, 0o644))
	f.config = filepath.Join(f.env.Data, "volumes", "deluge", "config")
	require.NoError(t, os.MkdirAll(filepath.Join(f.config, "plugins"), 0o755))
	writeConf(t, filepath.Join(f.config, "core.conf"), `{"file": 1, "format": 1}`, delugeCore)
	return f
}

func (f *delugeFixture) dockerAnswers(failing map[string]string) {
	record := func(call string) error {
		f.calls = append(f.calls, call)
		for prefix, detail := range failing {
			if strings.HasPrefix(call, prefix) {
				return errors.New(detail)
			}
		}
		return nil
	}
	f.docker.EXPECT().Exec(mock.Anything, "deluge", mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, _, user string, command []string) (string, error) {
		call := "exec " + strings.TrimSpace(user+" "+strings.Join(command, " "))
		if err := record(call); err != nil {
			return "", err
		}
		if user == "" && command[0] == "python3" {
			return "3.12\n", nil
		}
		if user == "abc" {
			require.NoError(f.t, os.WriteFile(filepath.Join(f.config, "plugins", "AutoRemovePlus-2.0.0-py3.12.egg"), nil, 0o644))
		}
		return "", nil
	}).Maybe()
	f.docker.EXPECT().Stop(mock.Anything, "deluge").RunAndReturn(func(context.Context, string) error { return record("stop deluge") }).Maybe()
	f.docker.EXPECT().Start(mock.Anything, "deluge").RunAndReturn(func(context.Context, string) error { return record("start deluge") }).Maybe()
}

func (f *delugeFixture) wire() error {
	return Deluge(f.docker)(context.Background(), f.env)
}

func (f *delugeFixture) restarts() []string {
	var restarts []string
	for _, call := range f.calls {
		if call == "stop deluge" || call == "start deluge" {
			restarts = append(restarts, call)
		}
	}
	return restarts
}

func (f *delugeFixture) builds() []string {
	var builds []string
	for _, call := range f.calls {
		if strings.HasPrefix(call, "exec abc sh -c") {
			builds = append(builds, call)
		}
	}
	return builds
}

func writeConf(t *testing.T, path, header, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(header+body), 0o644))
}

func readConf(t *testing.T, path string) (map[string]any, map[string]any) {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	var header, body map[string]any
	require.NoError(t, decoder.Decode(&header))
	require.NoError(t, decoder.Decode(&body))
	return header, body
}

func passwordMatches(t *testing.T, config, password string) bool {
	t.Helper()
	_, body := readConf(t, filepath.Join(config, "web.conf"))
	sum := sha1.Sum([]byte(body["pwd_salt"].(string) + password)) //nolint:gosec // Deluge's own password hash
	return hex.EncodeToString(sum[:]) == body["pwd_sha1"]
}

func TestAFreshDelugeGetsItsPluginsSettingsAndWebPasswordWithOneRestart(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.dockerAnswers(nil)

	require.NoError(t, f.wire())

	_, core := readConf(t, filepath.Join(f.config, "core.conf"))
	assert.Equal(t, []any{"Label", "AutoRemovePlus"}, core["enabled_plugins"])
	assert.Equal(t, "/data/downloads", core["download_location"])
	assert.True(t, passwordMatches(t, f.config, "web pass"))
	header, web := readConf(t, filepath.Join(f.config, "web.conf"))
	assert.Equal(t, map[string]any{"file": float64(2), "format": float64(1)}, header)
	assert.Equal(t, false, web["first_login"])
	_, remove := readConf(t, filepath.Join(f.config, "autoremoveplus.conf"))
	assert.Equal(t, []any{float64(168), "or", true}, []any{remove["min"], remove["sel_func"], remove["remove_data"]})
	content, err := os.ReadFile(filepath.Join(f.config, "autoremoveplus.conf"))
	require.NoError(t, err)
	assert.Contains(t, string(content), `"min": 168.0`)
	for _, name := range []string{"web.conf", "autoremoveplus.conf"} {
		info, err := os.Stat(filepath.Join(f.config, name))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), name)
	}
	assert.Equal(t, []string{"stop deluge", "start deluge"}, f.restarts())
	assert.Contains(t, f.out.lines, "deluge: enable plugin AutoRemovePlus")
	assert.Contains(t, f.out.lines, "deluge: set web password")
}

func TestTheWiringEndsBySigningInToTheWebUIWithThePassword(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.dockerAnswers(nil)

	require.NoError(t, f.wire())

	assert.Equal(t, map[string]any{"method": "auth.login", "params": []any{"web pass"}, "id": float64(1)}, f.r.sentBody("POST", "/json"))
}

func TestAMissingPluginEggIsBuiltInTheContainerFromThePinnedSourceBeforeTheRestart(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.dockerAnswers(nil)

	require.NoError(t, f.wire())

	builds := f.builds()
	require.Len(t, builds, 1)
	assert.Contains(t, builds[0], pluginSource)
	assert.Contains(t, builds[0], pluginSHA256)
	assert.Contains(t, builds[0], "sha256sum -c")
	build, stop := -1, -1
	for n, call := range f.calls {
		if call == builds[0] {
			build = n
		}
		if call == "stop deluge" {
			stop = n
		}
	}
	assert.Less(t, build, stop)
}

func TestAnEggBuiltForTheContainersPythonIsNotBuiltAgain(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	require.NoError(t, os.WriteFile(filepath.Join(f.config, "plugins", "AutoRemovePlus-2.0.0-py3.12.egg"), nil, 0o644))
	f.dockerAnswers(nil)

	require.NoError(t, f.wire())

	assert.Empty(t, f.builds())
}

func TestAnAlreadyWiredDelugeIsLeftUntouched(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.dockerAnswers(nil)
	require.NoError(t, f.wire())
	web, err := os.ReadFile(filepath.Join(f.config, "web.conf"))
	require.NoError(t, err)
	f.calls, f.out.lines = nil, nil

	require.NoError(t, f.wire())

	assert.Empty(t, f.restarts())
	assert.Empty(t, f.builds())
	after, err := os.ReadFile(filepath.Join(f.config, "web.conf"))
	require.NoError(t, err)
	assert.Equal(t, string(web), string(after))
	assert.Empty(t, f.out.lines)
}

func TestADriftedCoreSettingIsCorrectedAndTheRestOfCoreConfIsKept(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	require.NoError(t, os.WriteFile(filepath.Join(f.env.Config, "apps.yml"), []byte("deluge:\n  core:\n    max_upload_speed: 2000.0\n  plugins:\n    - name: Label\n"), 0o644))
	f.dockerAnswers(nil)
	require.NoError(t, f.wire())
	content, err := os.ReadFile(filepath.Join(f.config, "core.conf"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(f.config, "core.conf"), []byte(strings.Replace(string(content), `"max_upload_speed": 2000.0`, `"max_upload_speed": 500.0`, 1)), 0o600))
	f.calls = nil

	require.NoError(t, f.wire())

	_, core := readConf(t, filepath.Join(f.config, "core.conf"))
	assert.Equal(t, []any{float64(2000), "/downloads"}, []any{core["max_upload_speed"], core["download_location"]})
	assert.Equal(t, []string{"stop deluge", "start deluge"}, f.restarts())
	assert.Contains(t, f.out.lines, "deluge: set max_upload_speed 500.0 -> 2000.0")
}

func TestAChangedWebPasswordIsAppliedWithoutKnowingTheOldOne(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.dockerAnswers(nil)
	require.NoError(t, f.wire())
	f.env.Secrets["DELUGE_WEB_PASSWORD"] = "new pass"

	require.NoError(t, f.wire())

	assert.True(t, passwordMatches(t, f.config, "new pass"))
}

func TestPluginsEnabledByHandAreKept(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	writeConf(t, filepath.Join(f.config, "core.conf"), `{"file": 1, "format": 1}`, strings.Replace(delugeCore, `"enabled_plugins": []`, `"enabled_plugins": ["Scheduler"]`, 1))
	f.dockerAnswers(nil)

	require.NoError(t, f.wire())

	_, core := readConf(t, filepath.Join(f.config, "core.conf"))
	assert.Equal(t, []any{"Scheduler", "Label", "AutoRemovePlus"}, core["enabled_plugins"])
}

func TestAFailedPluginBuildFailsTheStepAfterTheSettingsAreApplied(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.dockerAnswers(map[string]string{"exec abc sh -c": "sha256sum: WARNING: 1 computed checksum did NOT match"})

	err := f.wire()

	assert.EqualError(t, err, "could not build plugin AutoRemovePlus: docker exec deluge failed: sha256sum: WARNING: 1 computed checksum did NOT match")
	assert.True(t, passwordMatches(t, f.config, "web pass"))
}

func TestAWebUIThatNeverAcceptsThePasswordFailsTheStep(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.r.on("POST", "/json", ok(`{"result": false, "error": null, "id": 1}`))
	f.dockerAnswers(nil)

	err := f.wire()

	assert.EqualError(t, err, "the web UI does not accept the web password")
}

func TestAWebUIThatDropsConnectionsWhileStartingIsWaitedFor(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.r.on("POST", "/json", answer{err: syscall.ECONNRESET}, answer{err: syscall.ECONNRESET}, ok(loggedIn))
	f.dockerAnswers(nil)

	require.NoError(t, f.wire())

	assert.Len(t, f.r.requests, 3)
}

func TestADockerCommandThatFailsEndsTheStepWithDockersError(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.dockerAnswers(map[string]string{"stop deluge": "Error response from daemon: No such container: deluge"})

	err := f.wire()

	assert.EqualError(t, err, "docker stop deluge failed: Error response from daemon: No such container: deluge")
}

func (f *delugeFixture) coreConfCannotBeWritten() {
	f.docker.EXPECT().Stop(mock.Anything, "deluge").RunAndReturn(func(context.Context, string) error {
		f.calls = append(f.calls, "stop deluge")
		return os.Chmod(filepath.Join(f.config, "core.conf"), 0o400)
	}).Once()
}

func TestAConfigDelugeCannotHaveWrittenIsReportedAndDelugeIsStillStartedAgain(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.coreConfCannotBeWritten()
	f.dockerAnswers(nil)

	err := f.wire()

	assert.EqualError(t, err, "could not write core.conf: permission denied")
	assert.Equal(t, []string{"stop deluge", "start deluge"}, f.restarts())
}

func TestAFailedWriteIsStillReportedWhenDelugeWillNotStartAgainEither(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.coreConfCannotBeWritten()
	f.dockerAnswers(map[string]string{"start deluge": "Error response from daemon: No such container: deluge"})

	err := f.wire()

	assert.EqualError(t, err, "could not write core.conf: permission denied; docker start deluge failed: Error response from daemon: No such container: deluge")
}

func TestAWebUIThatRefusesConnectionsIsAskedOncePerTry(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.r.on("POST", "/json", answer{err: syscall.ECONNREFUSED})
	f.dockerAnswers(nil)

	err := f.wire()

	assert.EqualError(t, err, "the web UI does not accept the web password")
	assert.Len(t, f.r.requests, delugeReadyTries)
}

func TestASignalWhileDelugeStopsStillStartsItAgain(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	ctx, cancel := context.WithCancel(context.Background())
	started := false
	f.docker.EXPECT().Exec(mock.Anything, "deluge", mock.Anything, mock.Anything).Return("3.12\n", nil).Maybe()
	f.docker.EXPECT().Stop(mock.Anything, "deluge").RunAndReturn(func(stopping context.Context, _ string) error {
		cancel()
		return stopping.Err()
	})
	f.docker.EXPECT().Start(mock.Anything, "deluge").RunAndReturn(func(context.Context, string) error { started = true; return nil })

	err := Deluge(f.docker)(ctx, f.env)

	assert.ErrorIs(t, err, context.Canceled)
	assert.True(t, started)
}

func TestADeclaredNullThatDelugeDoesNotHaveIsNoChange(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	require.NoError(t, os.WriteFile(filepath.Join(f.env.Config, "apps.yml"), []byte("deluge:\n  core:\n    proxy: null\n"), 0o644))
	f.dockerAnswers(nil)

	require.NoError(t, f.wire())

	assert.NotContains(t, f.out.lines, "deluge: set proxy None -> None")
	_, core := readConf(t, filepath.Join(f.config, "core.conf"))
	assert.NotContains(t, core, "proxy")
}
