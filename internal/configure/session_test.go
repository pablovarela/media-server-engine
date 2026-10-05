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

func formFor(v Values, section Section, problem string) Form {
	form := Form{Values: map[string]string{}, Stored: map[string]bool{}, Problem: problem}
	for _, f := range section.Fields {
		value := v.Get(f.File, f.Key)
		if f.Masked {
			form.Stored[f.Key] = value != ""
			value = ""
		}
		form.Values[f.Key] = value
	}
	return form
}

func problem(text string) any {
	return mock.MatchedBy(func(form Form) bool { return form.Problem == text })
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
	p.EXPECT().Section("General", fieldsOf("General"), formFor(current, sectionNamed("General"), "")).Return(general(current, "Europe/Madrid"), nil).Once()
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
	assert.Equal(t, Outcome{NothingChanged: true}, outcome)
}

func TestDecliningToSaveGoesBackToTheMenu(t *testing.T) {
	p := newMockPrompter(t)
	current := complete()
	expectMenu(p, "General", "save", "quit")
	p.EXPECT().Section("General", mock.Anything, problem("")).Return(general(current, "Europe/Madrid"), nil).Once()
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
	p.EXPECT().Section("General", mock.Anything, problem("")).Return(general(current, "Europe/Madrid"), nil).Once()
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
	p.EXPECT().Section("VPN", mock.Anything, problem("")).Return(nil, ErrAborted).Once()

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
	p.EXPECT().Section("VPN", mock.Anything, problem("")).Return(answer, nil).Once()
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
	p.EXPECT().Section("General", mock.Anything, problem("")).Return(bad, nil).Once()
	p.EXPECT().Section("General", mock.Anything, problem("TZ: Europe/Madird isn't a time zone")).Return(general(current, "Europe/Madrid"), nil).Once()
	p.EXPECT().Confirm("Save, commit and push these?", mock.Anything).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", current, fixedKey)

	require.NoError(t, err)
	assert.Equal(t, "Europe/Madrid", outcome.Values.Get(PlainFile, "TZ"))
}

func TestAFreshInstallationWalksTheMissingSectionsFirst(t *testing.T) {
	p := newMockPrompter(t)
	full := complete()
	var asked []string
	p.EXPECT().Section(mock.Anything, mock.Anything, problem("")).RunAndReturn(func(title string, _ []Field, _ Form) (map[string]string, error) {
		asked = append(asked, title)
		return currentOf(full, sectionNamed(title)), nil
	}).Times(4)
	expectMenu(p, "save")
	p.EXPECT().Confirm("Save, commit and push these?", mock.Anything).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", Values{}, fixedKey)

	require.NoError(t, err)
	assert.Equal(t, []string{"General", "Backups", "VPN", "App logins"}, asked)
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

func TestAB2RepositoryWithoutKeysReopensTheBackupsSection(t *testing.T) {
	p := newMockPrompter(t)
	current := complete().With(PlainFile, "RESTIC_REPOSITORY", "/mnt/backup").
		With(backupFile, "B2_ACCOUNT_ID", "").With(backupFile, "B2_ACCOUNT_KEY", "")
	toB2 := currentOf(current, sectionNamed("Backups"))
	toB2["RESTIC_REPOSITORY"] = "b2:bucket:gorgon"
	toB2["RESTIC_PASSWORD"] = ""
	withKeys := map[string]string{"RESTIC_REPOSITORY": "b2:bucket:gorgon", "RESTIC_PASSWORD": "", "B2_ACCOUNT_ID": "id", "B2_ACCOUNT_KEY": "key"}
	expectMenu(p, "Backups", "save")
	p.EXPECT().Section("Backups", mock.Anything, problem("")).Return(toB2, nil).Once()
	p.EXPECT().Section("Backups", mock.Anything, problem("B2_ACCOUNT_ID: needed for a b2: repository")).Return(withKeys, nil).Once()
	p.EXPECT().Confirm("Save, commit and push these?", mock.Anything).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", current, fixedKey)

	require.NoError(t, err)
	assert.Equal(t, "id", outcome.Values.Get(backupFile, "B2_ACCOUNT_ID"))
	assert.Equal(t, "current-RESTIC_PASSWORD", outcome.Values.Get(backupFile, "RESTIC_PASSWORD"))
}

func TestAReopenedSectionStillKeepsTheSecretsLeftEmpty(t *testing.T) {
	p := newMockPrompter(t)
	current := complete().With(AppsFile, "PORTAINER_ADMIN_PASSWORD", "too-short")
	keepAll := map[string]string{"JELLYFIN_ADMIN_PASSWORD": "", "DELUGE_WEB_PASSWORD": "", "PORTAINER_ADMIN_PASSWORD": ""}
	stored := formFor(current, sectionNamed("App logins"), "")
	expectMenu(p, "App logins", "save")
	p.EXPECT().Section("App logins", mock.Anything, stored).Return(keepAll, nil).Once()
	p.EXPECT().Section("App logins", mock.Anything, formFor(current, sectionNamed("App logins"), "PORTAINER_ADMIN_PASSWORD: needs at least 12 characters")).
		Return(map[string]string{"JELLYFIN_ADMIN_PASSWORD": "", "DELUGE_WEB_PASSWORD": "", "PORTAINER_ADMIN_PASSWORD": "long-enough-now"}, nil).Once()
	p.EXPECT().Confirm("Save, commit and push these?", []string{"App logins    PORTAINER_ADMIN_PASSWORD changed"}).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", current, fixedKey)

	require.NoError(t, err)
	assert.Equal(t, "current-JELLYFIN_ADMIN_PASSWORD", outcome.Values.Get(AppsFile, "JELLYFIN_ADMIN_PASSWORD"))
}

func TestASecretTypedBeforeAFailedCheckIsShownAgainAndKept(t *testing.T) {
	p := newMockPrompter(t)
	current := complete().With(PlainFile, "RESTIC_REPOSITORY", "/mnt/backup").
		With(backupFile, "B2_ACCOUNT_ID", "").With(backupFile, "B2_ACCOUNT_KEY", "")
	typed := map[string]string{"RESTIC_REPOSITORY": "b2:bucket:gorgon", "RESTIC_PASSWORD": "n3w-pass", "B2_ACCOUNT_ID": "", "B2_ACCOUNT_KEY": ""}
	reopened := Form{Values: typed, Stored: map[string]bool{"RESTIC_PASSWORD": true, "B2_ACCOUNT_ID": false, "B2_ACCOUNT_KEY": false},
		Problem: "B2_ACCOUNT_ID: needed for a b2: repository"}
	expectMenu(p, "Backups", "save")
	p.EXPECT().Section("Backups", mock.Anything, problem("")).Return(typed, nil).Once()
	p.EXPECT().Section("Backups", mock.Anything, reopened).
		Return(map[string]string{"RESTIC_REPOSITORY": "b2:bucket:gorgon", "RESTIC_PASSWORD": "n3w-pass", "B2_ACCOUNT_ID": "id", "B2_ACCOUNT_KEY": "key"}, nil).Once()
	p.EXPECT().Confirm("Save, commit and push these?", mock.Anything).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", current, fixedKey)

	require.NoError(t, err)
	assert.Equal(t, "n3w-pass", outcome.Values.Get(backupFile, "RESTIC_PASSWORD"))
}

func TestADashClearsAnOptionalSecret(t *testing.T) {
	p := newMockPrompter(t)
	current := complete()
	answer := formFor(current, sectionNamed("VPN"), "").Values
	answer["OPENVPN_USER"] = "-"
	answer["OPENVPN_PASSWORD"] = "-"
	expectMenu(p, "VPN", "save")
	p.EXPECT().Section("VPN", mock.Anything, problem("")).Return(answer, nil).Once()
	p.EXPECT().Confirm("Save, commit and push these?", []string{"VPN           OPENVPN_USER removed", "VPN           OPENVPN_PASSWORD removed"}).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", current, fixedKey)

	require.NoError(t, err)
	assert.Empty(t, outcome.Values.Get(vpnFile, "OPENVPN_USER"))
	assert.Empty(t, outcome.Values.Get(vpnFile, "OPENVPN_PASSWORD"))
}

func TestADashInARequiredSecretIsAValue(t *testing.T) {
	p := newMockPrompter(t)
	current := complete()
	answer := formFor(current, sectionNamed("Backups"), "").Values
	answer["RESTIC_PASSWORD"] = "-"
	expectMenu(p, "Backups", "save")
	p.EXPECT().Section("Backups", mock.Anything, problem("")).Return(answer, nil).Once()
	p.EXPECT().Confirm("Save, commit and push these?", mock.Anything).Return(true, nil).Once()

	outcome, err := Session(context.Background(), p, "gorgon", current, fixedKey)

	require.NoError(t, err)
	assert.Equal(t, "-", outcome.Values.Get(backupFile, "RESTIC_PASSWORD"))
}
