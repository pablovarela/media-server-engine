package machine

import (
	"context"
	"slices"
	"strings"

	"github.com/pablovarela/media-server-engine/internal/process"
)

const (
	gitCheck     = "git"
	ghCheck      = "gh"
	helperCheck  = "helper"
	dockerCheck  = "docker"
	groupCheck   = "group"
	sessionCheck = "session"
	resticCheck  = "restic"
	lingerCheck  = "linger"
	managerCheck = "manager"
)

const systemdPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

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
		{id: helperCheck, name: named("git uses gh for github.com"), needs: []string{gitCheck, ghCheck}, probe: gitUsesGh},
		{id: dockerCheck, name: named("Docker"), probe: dockerInstalled},
		{id: groupCheck, name: inGroupName, needs: []string{dockerCheck}, applies: linux, probe: inDockerGroup},
		{id: sessionCheck, name: named("Docker answers"), needs: []string{dockerCheck, groupCheck}, probe: dockerAnswers},
		{id: resticCheck, name: named("restic"), probe: resticInstalled},
		{id: lingerCheck, name: named("lingering"), applies: systemd, probe: lingering},
		{id: managerCheck, name: named("the user manager has the docker group"), needs: []string{dockerCheck, groupCheck, lingerCheck}, applies: systemd, probe: managerHasDocker},
	}
}

func output(ctx context.Context, env Env, name string, args ...string) (string, bool) {
	result, err := env.Runner.Output(ctx, process.Command{Name: name, Args: args})
	if err != nil || result.Exit != 0 {
		return "", false
	}
	return strings.TrimSpace(string(result.Stdout)), true
}

func gitInstalled(ctx context.Context, env Env) outcome {
	_, ok := output(ctx, env, "git", "--version")
	return outcome{ok: ok, line: "git isn't installed", fix: onOS(env, "sudo apt install git", "xcode-select --install")}.passing("git")
}

func ghLoggedIn(ctx context.Context, env Env) outcome {
	bare := []string{"-i", "HOME=" + env.Home, "USER=" + env.Account, "PATH=" + systemdPath, "gh", "auth", "token"}
	if token, ok := output(ctx, env, "env", bare...); ok && token != "" {
		return outcome{ok: true, line: "gh is logged in (token on disk)"}
	}
	if token, ok := output(ctx, env, "gh", "auth", "token"); ok && token != "" {
		return outcome{line: "gh's token is only in a keyring or GITHUB_TOKEN, which the timers can't read", fix: "gh auth login --insecure-storage"}
	}
	return outcome{line: "gh isn't logged in", fix: "gh auth login"}
}

func gitUsesGh(ctx context.Context, env Env) outcome {
	helpers, _ := output(ctx, env, "git", "config", "--global", "--get-all", "credential.https://github.com.helper")
	ok := strings.Contains(helpers, "gh auth git-credential")
	return outcome{ok: ok, line: "git doesn't use gh for github.com", fix: "gh auth setup-git"}.passing("git uses gh for github.com")
}

func dockerInstalled(ctx context.Context, env Env) outcome {
	_, ok := output(ctx, env, "docker", "--version")
	return outcome{ok: ok, line: "Docker isn't installed", fix: onOS(env, "curl -fsSL https://get.docker.com | sudo sh", "install OrbStack or Docker Desktop")}.passing("Docker")
}

func inGroupName(env Env) string { return env.Account + " is in the docker group" }

func inDockerGroup(ctx context.Context, env Env) outcome {
	groups, _ := output(ctx, env, "id", "-Gn", env.Account)
	ok := slices.Contains(strings.Fields(groups), "docker")
	return outcome{ok: ok, line: env.Account + " isn't in the docker group", fix: "sudo usermod -aG docker " + env.Account + ", then log out and back in"}.passing(inGroupName(env))
}

func dockerAnswers(ctx context.Context, env Env) outcome {
	_, ok := output(ctx, env, "docker", "info")
	return outcome{ok: ok, line: "Docker doesn't answer this session", fix: onOS(env, "log out and back in, so this session has the docker group", "start Docker")}.passing("Docker answers")
}

func resticInstalled(ctx context.Context, env Env) outcome {
	_, ok := output(ctx, env, "restic", "version")
	return outcome{ok: ok, line: "restic isn't installed", fix: onOS(env, "sudo apt install restic", "brew install restic")}.passing("restic")
}

func lingering(ctx context.Context, env Env) outcome {
	linger, _ := output(ctx, env, "loginctl", "show-user", env.Account, "-p", "Linger")
	return outcome{ok: linger == "Linger=yes", line: "lingering is off", fix: "sudo loginctl enable-linger " + env.Account}.passing("lingering")
}

func managerHasDocker(ctx context.Context, env Env) outcome {
	uid, stale := ManagerWithoutDocker(ctx, env.Runner, env.ProcRoot, env.Account)
	return outcome{ok: !stale, line: "the user manager started before " + env.Account + " joined the docker group",
		fix: "sudo systemctl restart user@" + uid + " (or reboot)"}.passing("the user manager has the docker group")
}

func (o outcome) passing(name string) outcome {
	if o.ok {
		return outcome{ok: true, line: name}
	}
	return o
}
