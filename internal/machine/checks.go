package machine

import (
	"context"
	"slices"
	"strings"

	"github.com/pablovarela/media-server-engine/internal/process"
)

const notInstalled = "not installed"

const (
	gitCheck     = "git"
	ghCheck      = "gh"
	helperCheck  = "helper"
	dockerCheck  = "docker"
	groupCheck   = "group"
	sessionCheck = "session"
	lingerCheck  = "linger"
	managerCheck = "manager"
)

func named(name string) func(Env) string { return func(Env) string { return name } }

func linux(env Env) bool { return env.GOOS == "linux" }

func systemd(env Env) bool { return env.Systemd }

func onOS(env Env, linuxFix, macFix string) string {
	if linux(env) {
		return linuxFix
	}
	return macFix
}

func checks() []check {
	return []check{
		{id: gitCheck, name: named("git"), probe: gitInstalled},
		{id: ghCheck, name: named("gh is logged in"), probe: ghLoggedIn},
		{id: helperCheck, name: named("git uses gh for github.com"), needs: []string{gitCheck}, probe: gitUsesGh},
		{id: dockerCheck, name: named("Docker"), probe: dockerInstalled},
		{id: groupCheck, name: inGroupName, needs: []string{dockerCheck}, applies: linux, probe: inDockerGroup},
		{id: sessionCheck, name: named("Docker answers"), needs: []string{dockerCheck, groupCheck}, probe: dockerAnswers},
		{id: lingerCheck, name: named("lingering"), applies: systemd, probe: lingering},
		{id: managerCheck, name: named("the user manager has the docker group"), needs: []string{dockerCheck, groupCheck, lingerCheck}, applies: systemd, probe: managerHasDocker},
	}
}

func output(ctx context.Context, r runner, name string, args ...string) (string, bool) {
	result, err := r.Output(ctx, process.Command{Name: name, Args: args})
	if err != nil || result.Exit != 0 {
		return "", false
	}
	return strings.TrimSpace(string(result.Stdout)), true
}

func gitInstalled(ctx context.Context, env Env) outcome {
	_, ok := output(ctx, env.Runner, "git", "--version")
	return outcome{ok: ok, detail: notInstalled, fix: onOS(env, "sudo apt install git", "xcode-select --install")}.passing()
}

func gitUsesGh(ctx context.Context, env Env) outcome {
	helpers, _ := output(ctx, env.Runner, "git", "config", "--global", "--get-all", "credential.https://github.com.helper")
	ok := strings.Contains(helpers, "gh auth git-credential")
	return outcome{ok: ok, fix: "gh auth setup-git"}.passing()
}

func dockerInstalled(ctx context.Context, env Env) outcome {
	_, ok := output(ctx, env.Runner, "docker", "--version")
	return outcome{ok: ok, detail: notInstalled, fix: onOS(env, "curl -fsSL https://get.docker.com | sudo sh", "install OrbStack or Docker Desktop")}.passing()
}

func inGroupName(env Env) string { return env.Account + " is in the docker group" }

func inDockerGroup(ctx context.Context, env Env) outcome {
	groups, _ := output(ctx, env.Runner, "id", "-Gn", env.Account)
	ok := slices.Contains(strings.Fields(groups), "docker")
	return outcome{ok: ok, fix: "sudo usermod -aG docker " + env.Account + ", then log out and back in"}.passing()
}

func dockerAnswers(ctx context.Context, env Env) outcome {
	if _, ok := output(ctx, env.Runner, "docker", "info"); ok {
		return outcome{ok: true}
	}
	if !linux(env) {
		return outcome{fix: "start Docker"}
	}
	if session, _ := output(ctx, env.Runner, "id", "-Gn"); slices.Contains(strings.Fields(session), "docker") {
		return outcome{detail: "the daemon doesn't answer", fix: "sudo systemctl start docker"}
	}
	return outcome{detail: "this session doesn't have the docker group yet", fix: "log out and back in"}
}

func lingering(ctx context.Context, env Env) outcome {
	linger, _ := output(ctx, env.Runner, "loginctl", "show-user", env.Account, "-p", "Linger")
	return outcome{ok: linger == "Linger=yes", detail: "off", fix: "sudo loginctl enable-linger " + env.Account}.passing()
}

func managerHasDocker(ctx context.Context, env Env) outcome {
	uid, stale := ManagerWithoutDocker(ctx, env.Runner, env.ProcRoot, env.Account)
	return outcome{ok: !stale, detail: "it started before " + env.Account + " joined the docker group",
		fix: "sudo systemctl restart user@" + uid + " (or reboot)"}.passing()
}

func (o outcome) passing() outcome {
	if o.ok {
		return outcome{ok: true}
	}
	return o
}
