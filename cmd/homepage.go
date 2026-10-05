package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/spf13/cobra"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/homepage"
	"github.com/pablovarela/media-server-engine/internal/installation"
)

const homepageService = "homepage"

type pageChanges struct {
	env    bool
	images bool
}

func newHomepageCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "homepage",
		Short: "Redraw the landing page from the config's homepage files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := deps.openProject(cmd, compose.Stack, drawingAlways)
			if err != nil {
				return err
			}
			if err := deps.applyPage(cmd.Context(), o); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "The landing page is redrawn; an open page reloads itself in a few seconds.")
			return err
		},
	}
}

func (d Dependencies) drawPage(ctx context.Context, i *installation.Installation, network string, warn io.Writer) (pageChanges, error) {
	hostname, err := d.Host.Hostname()
	if err != nil {
		return pageChanges{}, err
	}
	short, _, _ := strings.Cut(hostname, ".")
	inputs := homepage.Inputs{
		Network:         network,
		Version:         d.Build.Version,
		EngineURL:       homepage.EngineURL(d.Build),
		Role:            i.Role(),
		ShortHost:       short,
		HealthchecksKey: homepage.HealthchecksKey(i),
	}
	images, err := homepage.Draw(ctx, d.Engine, i, inputs, homepage.Healthchecks{Client: d.HTTP, URL: homepage.ChecksURL}, warn)
	if err != nil {
		return pageChanges{}, err
	}
	env, err := homepage.WriteEnv(i, homepage.Env(i))
	return pageChanges{env: env, images: images}, err
}

func (d Dependencies) applyPage(ctx context.Context, o opened) error {
	if o.page == nil {
		return nil
	}
	return reload(ctx, d, o.runner, o.project, o.installation.HomepagePort(), *o.page)
}

func reload(ctx context.Context, d Dependencies, runner composeRunner, project *types.Project, port string, changes pageChanges) error {
	containers, err := runner.Ps(ctx, project)
	if err != nil || !slices.ContainsFunc(containers, func(c compose.Container) bool { return c.Name == homepageService && c.State == "running" }) {
		return nil
	}
	switch {
	case changes.env:
		return runner.Up(ctx, project, []string{homepageService})
	case changes.images:
		return runner.Restart(ctx, project, []string{homepageService})
	}
	revalidate(ctx, d.HTTP, "http://localhost:"+port+"/api/revalidate")
	return nil
}

func revalidate(ctx context.Context, client *http.Client, url string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	if response, err := client.Do(request); err == nil {
		_ = response.Body.Close()
	}
}
