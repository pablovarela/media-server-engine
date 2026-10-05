package wiring

import (
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // Deluge's own password hash
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	delugeApp        = "deluge"
	delugeContainer  = "deluge"
	delugeReadyTries = 60
)

type Docker interface {
	Exec(ctx context.Context, container, user string, command []string) (string, error)
	Stop(ctx context.Context, container string) error
	Start(ctx context.Context, container string) error
}

type deluge struct {
	env      Env
	docker   Docker
	config   string
	password string
	core     *delugeConf
	web      *delugeConf
	plugins  []*delugeConf
	failures []string
}

func Deluge(docker Docker) func(ctx context.Context, env Env) error {
	return func(ctx context.Context, env Env) error {
		declared, err := env.Declared("apps.yml")
		if err != nil {
			return err
		}
		config := section(declared[delugeApp])
		plugins := entries(config["plugins"])
		d, err := newDeluge(env, docker)
		if err != nil {
			return err
		}
		if err := d.ensurePluginEggs(ctx, plugins); err != nil {
			return err
		}
		d.wireCore(section(config["core"]), plugins)
		if err := d.wirePluginSettings(plugins); err != nil {
			return err
		}
		if err := d.wireWebPassword(); err != nil {
			return err
		}
		if err := d.apply(ctx); err != nil {
			return err
		}
		if err := d.waitForWebLogin(ctx); err != nil {
			return err
		}
		if len(d.failures) > 0 {
			return Error{Message: strings.Join(d.failures, "; ")}
		}
		return nil
	}
}

func newDeluge(env Env, docker Docker) (*deluge, error) {
	d := &deluge{env: env, docker: docker, config: filepath.Join(env.Data, "volumes", "deluge", "config"), password: env.Secrets["DELUGE_WEB_PASSWORD"]}
	var err error
	if d.core, err = readDelugeConf(filepath.Join(d.config, "core.conf"), pluginConfHeader); err != nil {
		return nil, err
	}
	if d.web, err = readDelugeConf(filepath.Join(d.config, "web.conf"), webConfHeader); err != nil {
		return nil, err
	}
	return d, nil
}

func dockerFailure(verb string, err error) error {
	return Error{Message: fmt.Sprintf("docker %s %s failed: %v", verb, delugeContainer, err)}
}

func (d *deluge) ensurePluginEggs(ctx context.Context, plugins []map[string]any) error {
	var withSource []map[string]any
	for _, plugin := range plugins {
		if text(plugin["source"]) != "" {
			withSource = append(withSource, plugin)
		}
	}
	if len(withSource) == 0 {
		return nil
	}
	version, err := d.docker.Exec(ctx, delugeContainer, "", []string{"python3", "-c", `import sys; print("%d.%d" % sys.version_info[:2])`})
	if err != nil {
		return dockerFailure("exec", err)
	}
	python := strings.TrimSpace(version)
	for _, plugin := range withSource {
		name := text(plugin[nameKey])
		if eggs, _ := filepath.Glob(filepath.Join(d.config, "plugins", name+"-*-py"+python+".egg")); len(eggs) > 0 {
			continue
		}
		d.env.Change(delugeApp, fmt.Sprintf("build plugin %s for python %s", name, python))
		if _, err := d.docker.Exec(ctx, delugeContainer, "abc", []string{"sh", "-c", buildScript(plugin)}); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			d.failures = append(d.failures, fmt.Sprintf("could not build plugin %s: %v", name, dockerFailure("exec", err)))
		}
	}
	return nil
}

func buildScript(plugin map[string]any) string {
	return `set -e; d=$(mktemp -d); trap "rm -rf $d" EXIT; cd "$d"; ` +
		fmt.Sprintf(`curl -fsSL -o src.tgz "%s"; `, text(plugin["source"])) +
		fmt.Sprintf(`echo "%s  src.tgz" | sha256sum -c -; `, text(plugin["sha256"])) +
		`tar xzf src.tgz; cd */; python3 setup.py -q bdist_egg; cp dist/*.egg /config/plugins/`
}

func (d *deluge) wireCore(settings map[string]any, plugins []map[string]any) {
	for _, key := range sortedKeys(settings) {
		value := pythonValue(settings[key])
		if current, ok := d.core.body[key]; !ok || !same(current, value) {
			d.env.Change(delugeApp, fmt.Sprintf("set %s %s -> %s", key, show(d.core.body[key]), show(value)))
			d.core.set(key, value)
		}
	}
	enabled := append([]any{}, list(d.core.body["enabled_plugins"])...)
	for _, plugin := range plugins {
		name := text(plugin[nameKey])
		if !contains(enabled, name) {
			d.env.Change(delugeApp, "enable plugin "+name)
			enabled = append(enabled, name)
			d.core.set("enabled_plugins", enabled)
		}
	}
}

func (d *deluge) wirePluginSettings(plugins []map[string]any) error {
	for _, plugin := range plugins {
		settings := section(plugin["settings"])
		if len(settings) == 0 {
			continue
		}
		name := text(plugin[nameKey])
		conf, err := readDelugeConf(filepath.Join(d.config, strings.ToLower(name)+".conf"), pluginConfHeader)
		if err != nil {
			return err
		}
		for _, key := range sortedKeys(settings) {
			value := pythonValue(settings[key])
			if current, ok := conf.body[key]; !ok || !same(current, value) {
				d.env.Change(delugeApp, fmt.Sprintf("set %s %s %s -> %s", name, key, show(conf.body[key]), show(value)))
				conf.set(key, value)
			}
		}
		d.plugins = append(d.plugins, conf)
	}
	return nil
}

func passwordHash(salt, password string) string {
	sum := sha1.Sum([]byte(salt + password)) //nolint:gosec // Deluge's own password hash
	return hex.EncodeToString(sum[:])
}

func (d *deluge) wireWebPassword() error {
	body := d.web.body
	if salt := text(body["pwd_salt"]); salt != "" && passwordHash(salt, d.password) == text(body["pwd_sha1"]) {
		if body["first_login"] != false {
			d.env.Change(delugeApp, "turn off the first login prompt")
			d.web.set("first_login", false)
		}
		return nil
	}
	d.env.Change(delugeApp, "set web password")
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	saltSum := sha1.Sum(random) //nolint:gosec // Deluge's salt is a sha1 hex digest
	salt := hex.EncodeToString(saltSum[:])
	d.web.set("pwd_salt", salt)
	d.web.set("pwd_sha1", passwordHash(salt, d.password))
	d.web.set("first_login", false)
	return nil
}

func (d *deluge) apply(ctx context.Context) error {
	var changed []*delugeConf
	for _, conf := range append([]*delugeConf{d.core, d.web}, d.plugins...) {
		if conf.dirty {
			changed = append(changed, conf)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	if err := d.docker.Stop(ctx, delugeContainer); err != nil {
		return dockerFailure("stop", err)
	}
	var failures []string
	for _, conf := range changed {
		if err := conf.save(); err != nil {
			failures = append(failures, fmt.Sprintf("could not write %s: %v", filepath.Base(conf.path), err))
			break
		}
	}
	if err := d.docker.Start(context.WithoutCancel(ctx), delugeContainer); err != nil {
		failures = append(failures, dockerFailure("start", err).Error())
	}
	if len(failures) > 0 {
		return Error{Message: strings.Join(failures, "; ")}
	}
	return nil
}

func (d *deluge) waitForWebLogin(ctx context.Context) error {
	api := &API{Env: d.env, Base: d.env.URL("DELUGE_URL", "http://localhost:8112"), Headers: map[string]string{}}
	for try := 0; try < delugeReadyTries; try++ {
		var answer map[string]any
		err := api.Send(ctx, "POST", "/json", map[string]any{"method": "auth.login", "params": []any{d.password}, "id": 1}, &answer)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && answer["result"] == true {
			return nil
		}
		if err := d.env.Pause(ctx, time.Second); err != nil {
			return err
		}
	}
	return Error{Message: "the web UI does not accept the web password"}
}
