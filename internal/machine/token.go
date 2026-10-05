package machine

import (
	"context"
	"strings"

	"github.com/pablovarela/media-server-engine/internal/process"
)

const SystemdPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

const notFound = 127

func UnattendedToken(ctx context.Context, r runner, home, account string, carried []string) bool {
	result, err := bareGhToken(ctx, r, home, account, carried)
	return err == nil && result.Exit == 0 && strings.TrimSpace(string(result.Stdout)) != ""
}

func bareGhToken(ctx context.Context, r runner, home, account string, carried []string) (process.Result, error) {
	args := append([]string{"-i", "HOME=" + home, "USER=" + account, "PATH=" + SystemdPath}, carried...)
	return r.Output(ctx, process.Command{Name: "env", Args: append(args, "gh", "auth", "token")})
}

func ghLoggedIn(ctx context.Context, env Env) outcome {
	if !env.Systemd {
		return shellToken(ctx, env, "gh is logged in")
	}
	if UnattendedToken(ctx, env.Runner, env.Home, env.Account, env.Carried) {
		return outcome{ok: true, line: "gh is logged in (token on disk)"}
	}
	if result, err := bareGhToken(ctx, env.Runner, env.Home, env.Account, env.Carried); err == nil && result.Exit == notFound {
		if _, found := output(ctx, env.Runner, "gh", "--version"); found {
			return outcome{line: "gh isn't on the timers' PATH (" + SystemdPath + ")", fix: `sudo ln -s "$(command -v gh)" /usr/local/bin/gh`}
		}
		return ghMissing(env)
	}
	shell := shellToken(ctx, env, "")
	if !shell.ok {
		return shell
	}
	if variable := tokenVariable(env); variable != "" {
		return outcome{line: "gh's token comes from " + variable + ", which the timers can't read", fix: "unset " + variable + ", then gh auth login --insecure-storage"}
	}
	return outcome{line: "gh's token is only in a keyring, which the timers can't read", fix: "gh auth login --insecure-storage"}
}

func shellToken(ctx context.Context, env Env, passing string) outcome {
	result, err := env.Runner.Output(ctx, process.Command{Name: "gh", Args: []string{"auth", "token"}})
	switch {
	case err != nil || result.Exit == notFound:
		return ghMissing(env)
	case result.Exit == 0 && strings.TrimSpace(string(result.Stdout)) != "":
		return outcome{ok: true, line: passing}
	}
	return outcome{line: "gh isn't logged in", fix: "gh auth login"}
}

func ghMissing(env Env) outcome {
	return outcome{line: "gh isn't installed", fix: onOS(env, "sudo apt install gh", "brew install gh")}
}

func tokenVariable(env Env) string {
	if env.Getenv == nil {
		return ""
	}
	var set []string
	for _, variable := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if env.Getenv(variable) != "" {
			set = append(set, variable)
		}
	}
	return strings.Join(set, " ")
}
