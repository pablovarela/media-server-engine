package configure

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func guidedAnswers() map[string]map[string]string {
	return map[string]map[string]string{
		"General":      {"TZ": "Europe/London", "JELLYFIN_ADMIN_USER": "pablo"},
		"Backups":      {"RESTIC_REPOSITORY": "/backups", "RESTIC_PASSWORD": "restic"},
		"VPN":          {"VPN_SERVICE_PROVIDER": "protonvpn"},
		"Healthchecks": {},
		"App logins":   {"JELLYFIN_ADMIN_PASSWORD": "j", "DELUGE_WEB_PASSWORD": "d", "PORTAINER_ADMIN_PASSWORD": "twelve-chars"},
	}
}

func answering(p *mockPrompter, section string, answer map[string]string) *mock.Call {
	return p.EXPECT().Section(section, mock.Anything, mock.Anything).RunAndReturn(
		func(_ string, fields []Field, form Form) (map[string]string, error) {
			merged := map[string]string{}
			for _, f := range fields {
				merged[f.Key] = form.Values[f.Key]
			}
			for k, v := range answer {
				merged[k] = v
			}
			return merged, nil
		}).Once()
}

func TestGuidedAsksEverySectionInOrderThenSaves(t *testing.T) {
	p := newMockPrompter(t)
	var asked []string
	for _, section := range Sections() {
		answering(p, section.Name, guidedAnswers()[section.Name]).Run(func(args mock.Arguments) { asked = append(asked, args.String(0)) })
	}
	before := Values{PlainFile: {"INSTALLATION_NAME": "gorgon"}}
	start := before.With(AppsFile, "SONARR_API_KEY", "k")
	p.EXPECT().Confirm("Save these, commit and push them to a new repository?", mock.Anything).RunAndReturn(func(_ string, lines []string) (bool, error) {
		assert.Contains(t, lines, "General       TZ: (none) -> Europe/London")
		assert.Contains(t, lines, "Rotate keys   SONARR_API_KEY new")
		return true, nil
	}).Once()

	outcome, err := Guided(context.Background(), p, before, start)

	require.NoError(t, err)
	assert.Equal(t, []string{"General", "Backups", "VPN", "Healthchecks", "App logins"}, asked)
	assert.True(t, outcome.Save)
	assert.Equal(t, "Europe/London", outcome.Values.Get(PlainFile, "TZ"))
	assert.Equal(t, "twelve-chars", outcome.Values.Get(AppsFile, "PORTAINER_ADMIN_PASSWORD"))
	assert.Equal(t, "k", outcome.Values.Get(AppsFile, "SONARR_API_KEY"))
}

func TestGuidedReturnsToASectionMissingARequiredField(t *testing.T) {
	p := newMockPrompter(t)
	answering(p, "General", map[string]string{"TZ": "Europe/London"})
	p.EXPECT().Section("General", mock.Anything, mock.MatchedBy(func(f Form) bool { return f.Problem != "" })).Return(
		map[string]string{"TZ": "Europe/London", "JELLYFIN_ADMIN_USER": "pablo"}, nil).Once()
	for _, section := range Sections()[1:] {
		answering(p, section.Name, guidedAnswers()[section.Name])
	}
	p.EXPECT().Confirm(mock.Anything, mock.Anything).Return(true, nil).Once()

	outcome, err := Guided(context.Background(), p, Values{}, Values{})

	require.NoError(t, err)
	assert.Equal(t, "pablo", outcome.Values.Get(PlainFile, "JELLYFIN_ADMIN_USER"))
}

func TestGuidedQuits(t *testing.T) {
	quitAt := map[string]func(p *mockPrompter){
		"esc in a section": func(p *mockPrompter) {
			answering(p, "General", guidedAnswers()["General"])
			p.EXPECT().Section("Backups", mock.Anything, mock.Anything).Return(nil, ErrAborted).Once()
		},
		"no at the summary": func(p *mockPrompter) {
			for _, section := range Sections() {
				answering(p, section.Name, guidedAnswers()[section.Name])
			}
			p.EXPECT().Confirm(mock.Anything, mock.Anything).Return(false, nil).Once()
		},
		"esc at the summary": func(p *mockPrompter) {
			for _, section := range Sections() {
				answering(p, section.Name, guidedAnswers()[section.Name])
			}
			p.EXPECT().Confirm(mock.Anything, mock.Anything).Return(false, ErrAborted).Once()
		},
	}
	for name, given := range quitAt {
		t.Run(name, func(t *testing.T) {
			p := newMockPrompter(t)
			given(p)

			outcome, err := Guided(context.Background(), p, Values{}, Values{})

			assert.ErrorIs(t, err, ErrAborted)
			assert.False(t, outcome.Save)
		})
	}
}
