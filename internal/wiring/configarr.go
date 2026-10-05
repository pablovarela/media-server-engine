package wiring

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const shownErrors = 3

var passwordLine = regexp.MustCompile(`([Pp]assword[^:]*:).*`)

type OneOff func(ctx context.Context, out io.Writer) (exit int, err error)

func Configarr(run OneOff, tool io.Writer) func(ctx context.Context, env Env) error {
	return func(ctx context.Context, env Env) error {
		var output bytes.Buffer
		exit, err := run(ctx, &output)
		var errors []string
		for _, raw := range outputLines(output.String()) {
			line := env.Redact.Hide(passwordLine.ReplaceAllString(raw, "$1 (hidden)"))
			_, _ = fmt.Fprintln(tool, line)
			if strings.HasPrefix(line, "ERROR") {
				errors = append(errors, line)
			}
		}
		switch {
		case err != nil:
			return err
		case exit != 0:
			return Error{Message: fmt.Sprintf("configarr exited with %d", exit)}
		case len(errors) > 0:
			return Error{Message: "configarr reported errors: " + strings.Join(errors[:min(shownErrors, len(errors))], " ")}
		}
		return nil
	}
}

func outputLines(output string) []string {
	if output == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(output, "\n"), "\n")
}
