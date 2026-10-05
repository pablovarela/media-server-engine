package configure

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const newKey = "0123456789abcdef0123456789abcdef"

func fixedKey() (string, error) { return newKey, nil }

func menuItems() []MenuItem {
	var items []MenuItem
	for _, section := range Sections() {
		items = append(items, MenuItem{ID: section.Name, Title: section.Name, Summary: section.Summary})
	}
	return append(items,
		MenuItem{ID: "rotate", Title: "Rotate keys", Summary: "Sonarr, Radarr, Prowlarr API keys"},
		MenuItem{ID: "save", Title: "Save and quit"},
		MenuItem{ID: "quit", Title: "Quit without saving"},
	)
}

func fieldsOf(name string) any {
	return mock.MatchedBy(func(fields []Field) bool {
		var want, got []string
		for _, f := range sectionNamed(name).Fields {
			want = append(want, f.Key)
		}
		for _, f := range fields {
			got = append(got, f.Key)
		}
		return assert.ObjectsAreEqual(want, got)
	})
}

func currentOf(v Values, section Section) map[string]string {
	current := map[string]string{}
	for _, f := range section.Fields {
		current[f.Key] = v.Get(f.File, f.Key)
	}
	return current
}

func expectMenu(p *mockPrompter, choices ...string) {
	for _, choice := range choices {
		p.EXPECT().Menu("Configure gorgon", menuItems()).Return(choice, nil).Once()
	}
}

func general(v Values, tz string) map[string]string {
	answer := currentOf(v, sectionNamed("General"))
	answer["TZ"] = tz
	return answer
}

func TestSavingAChangedTimeZone(t *testing.T) {
	p := newMockPrompter(t)
	current := complete()
	expectMenu(p, "General", "save")
	p.EXPECT().Section("General", fieldsOf("General"), currentOf(current, sectionNamed("General")), "").Return(general(current, "Europe/Madrid"), nil).Once()
	p.EXPECT().Confirm("Save, commit and push these?", []string{"General       TZ: Europe/London -> Europe/Madrid"}).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", current, fixedKey)

	require.NoError(t, err)
	assert.True(t, outcome.Save)
	assert.Equal(t, "Europe/Madrid", outcome.Values.Get(PlainFile, "TZ"))
}

func TestSavingWithNothingChangedAsksNothing(t *testing.T) {
	p := newMockPrompter(t)
	expectMenu(p, "save")

	outcome, err := Session(context.Background(), p, "gorgon", complete(), fixedKey)

	require.NoError(t, err)
	assert.False(t, outcome.Save)
}

func TestDecliningToSaveGoesBackToTheMenu(t *testing.T) {
	p := newMockPrompter(t)
	current := complete()
	expectMenu(p, "General", "save", "quit")
	p.EXPECT().Section("General", mock.Anything, mock.Anything, "").Return(general(current, "Europe/Madrid"), nil).Once()
	p.EXPECT().Confirm("Save, commit and push these?", mock.Anything).Return(false, nil).Once()
	p.EXPECT().Confirm("Discard 1 changes?", []string(nil)).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", current, fixedKey)

	require.NoError(t, err)
	assert.Equal(t, Outcome{}, outcome)
}

func TestKeepingTheChangesWhenAskedToDiscardThem(t *testing.T) {
	p := newMockPrompter(t)
	current := complete()
	expectMenu(p, "General")
	p.EXPECT().Section("General", mock.Anything, mock.Anything, "").Return(general(current, "Europe/Madrid"), nil).Once()
	p.EXPECT().Menu("Configure gorgon", menuItems()).Return("", ErrAborted).Once()
	p.EXPECT().Confirm("Discard 1 changes?", []string(nil)).Return(false, nil).Once()
	expectMenu(p, "save")
	p.EXPECT().Confirm("Save, commit and push these?", mock.Anything).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", current, fixedKey)

	require.NoError(t, err)
	assert.True(t, outcome.Save)
}

func TestQuittingWithoutChangesAsksNothing(t *testing.T) {
	p := newMockPrompter(t)
	expectMenu(p, "quit")

	outcome, err := Session(context.Background(), p, "gorgon", complete(), fixedKey)

	require.NoError(t, err)
	assert.Equal(t, Outcome{}, outcome)
}

func TestLeavingASectionKeepsItAsItWas(t *testing.T) {
	p := newMockPrompter(t)
	expectMenu(p, "VPN", "save")
	p.EXPECT().Section("VPN", mock.Anything, mock.Anything, "").Return(nil, ErrAborted).Once()

	outcome, err := Session(context.Background(), p, "gorgon", complete(), fixedKey)

	require.NoError(t, err)
	assert.False(t, outcome.Save)
}

func TestAnEmptyMaskedFieldKeepsTheCurrentSecret(t *testing.T) {
	p := newMockPrompter(t)
	current := complete()
	answer := currentOf(current, sectionNamed("VPN"))
	answer["OPENVPN_PASSWORD"] = ""
	answer["SERVER_COUNTRIES"] = "Spain"
	expectMenu(p, "VPN", "save")
	p.EXPECT().Section("VPN", mock.Anything, mock.Anything, "").Return(answer, nil).Once()
	p.EXPECT().Confirm("Save, commit and push these?", []string{"VPN           SERVER_COUNTRIES changed"}).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", current, fixedKey)

	require.NoError(t, err)
	assert.Equal(t, "current-OPENVPN_PASSWORD", outcome.Values.Get(vpnFile, "OPENVPN_PASSWORD"))
}

func TestABadValueReopensTheSectionWithWhatWasEntered(t *testing.T) {
	p := newMockPrompter(t)
	current := complete()
	bad := general(current, "Europe/Madird")
	expectMenu(p, "General", "save")
	p.EXPECT().Section("General", mock.Anything, currentOf(current, sectionNamed("General")), "").Return(bad, nil).Once()
	p.EXPECT().Section("General", mock.Anything, bad, "TZ: Europe/Madird isn't a time zone").Return(general(current, "Europe/Madrid"), nil).Once()
	p.EXPECT().Confirm("Save, commit and push these?", mock.Anything).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", current, fixedKey)

	require.NoError(t, err)
	assert.Equal(t, "Europe/Madrid", outcome.Values.Get(PlainFile, "TZ"))
}

func TestAFreshInstallationWalksTheMissingSectionsFirst(t *testing.T) {
	p := newMockPrompter(t)
	full := complete()
	var asked []string
	p.EXPECT().Section(mock.Anything, mock.Anything, mock.Anything, "").RunAndReturn(func(title string, _ []Field, _ map[string]string, _ string) (map[string]string, error) {
		asked = append(asked, title)
		return currentOf(full, sectionNamed(title)), nil
	}).Times(5)
	expectMenu(p, "save")
	p.EXPECT().Confirm("Save, commit and push these?", mock.Anything).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", Values{}, fixedKey)

	require.NoError(t, err)
	assert.Equal(t, []string{"General", "Backups", "VPN", "Healthchecks", "App logins"}, asked)
	assert.True(t, outcome.Save)
}

func TestRotatingTheSonarrKey(t *testing.T) {
	p := newMockPrompter(t)
	expectMenu(p, "rotate", "save")
	p.EXPECT().Rotate(Rotatable).Return([]App{Rotatable[0]}, nil).Once()
	p.EXPECT().Confirm("Save, commit and push these?", []string{"Rotate keys   SONARR_API_KEY new"}).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", complete(), fixedKey)

	require.NoError(t, err)
	assert.Equal(t, newKey, outcome.Values.Get(AppsFile, "SONARR_API_KEY"))
}

func TestAStoppedSessionReturnsTheContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Session(ctx, newMockPrompter(t), "gorgon", complete(), fixedKey)

	require.ErrorIs(t, err, context.Canceled)
}
