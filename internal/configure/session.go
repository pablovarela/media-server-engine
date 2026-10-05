package configure

import (
	"context"
	"errors"
	"fmt"
)

const (
	rotateItem = "rotate"
	saveItem   = "save"
	quitItem   = "quit"
)

type Outcome struct {
	Values         Values
	Save           bool
	NothingChanged bool
}

type session struct {
	ctx       context.Context
	prompter  Prompter
	title     string
	before    Values
	values    Values
	randomKey func() (string, error)
}

func Session(ctx context.Context, p Prompter, name string, current Values, randomKey func() (string, error)) (Outcome, error) {
	s := &session{ctx: ctx, prompter: p, title: "Configure " + name, before: current, values: current, randomKey: randomKey}
	for _, section := range Missing(current) {
		if err := s.edit(section); err != nil {
			return Outcome{}, err
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return Outcome{}, err
		}
		outcome, done, err := s.choose()
		if done || err != nil {
			return outcome, err
		}
	}
}

func (s *session) choose() (Outcome, bool, error) {
	choice, err := s.prompter.Menu(s.title, menu())
	switch {
	case errors.Is(err, ErrAborted) || choice == quitItem:
		return s.quit()
	case err != nil:
		return Outcome{}, true, err
	case choice == saveItem:
		return s.save()
	case choice == rotateItem:
		return Outcome{}, false, s.rotate()
	}
	return Outcome{}, false, s.edit(sectionNamed(choice))
}

func menu() []MenuItem {
	var items []MenuItem
	for _, section := range Sections() {
		items = append(items, MenuItem{ID: section.Name, Title: section.Name, Summary: section.Summary})
	}
	return append(items,
		MenuItem{ID: rotateItem, Title: rotateSection, Summary: "Sonarr, Radarr, Prowlarr API keys"},
		MenuItem{ID: saveItem, Title: "Save and quit"},
		MenuItem{ID: quitItem, Title: "Quit without saving"},
	)
}

func sectionNamed(name string) Section {
	for _, section := range Sections() {
		if section.Name == name {
			return section
		}
	}
	return Section{}
}

const removeSecret = "-"

func (s *session) edit(section Section) error {
	form := s.form(section)
	for {
		answer, err := s.prompter.Section(section.Name, section.Fields, form)
		if errors.Is(err, ErrAborted) {
			return nil
		}
		if err != nil {
			return err
		}
		updated, err := s.merged(section, answer)
		if err == nil {
			s.values = updated
			return nil
		}
		form = Form{Values: map[string]string{}, Stored: form.Stored, Problem: err.Error()}
		for _, f := range section.Fields {
			form.Values[f.Key] = answer[f.Key]
		}
	}
}

func (s *session) form(section Section) Form {
	form := Form{Values: map[string]string{}, Stored: map[string]bool{}}
	for _, f := range section.Fields {
		value := s.values.Get(f.File, f.Key)
		if f.Masked {
			form.Stored[f.Key] = value != ""
			value = ""
		}
		form.Values[f.Key] = value
	}
	return form
}

func (s *session) merged(section Section, answer map[string]string) (Values, error) {
	updated := s.values
	for _, f := range section.Fields {
		updated = updated.With(f.File, f.Key, s.entered(f, answer[f.Key]))
	}
	for _, f := range section.Fields {
		if err := f.ValidateIn(updated); err != nil {
			return nil, err
		}
	}
	return updated, nil
}

func (s *session) entered(f Field, value string) string {
	switch {
	case !f.Masked:
		return value
	case value == "":
		return s.values.Get(f.File, f.Key)
	case value == removeSecret && f.Optional:
		return ""
	}
	return value
}

func (s *session) rotate() error {
	apps, err := s.prompter.Rotate(Rotatable)
	if errors.Is(err, ErrAborted) {
		return nil
	}
	if err != nil {
		return err
	}
	rotated, err := Rotate(s.values, apps, s.randomKey)
	if err == nil {
		s.values = rotated
	}
	return err
}

func (s *session) save() (Outcome, bool, error) {
	changes := Diff(s.before, s.values)
	if len(changes) == 0 {
		return Outcome{NothingChanged: true}, true, nil
	}
	confirmed, err := s.prompter.Confirm("Save, commit and push these?", Summary(changes))
	if err != nil && !errors.Is(err, ErrAborted) {
		return Outcome{}, true, err
	}
	if !confirmed {
		return Outcome{}, false, nil
	}
	return Outcome{Values: s.values, Save: true}, true, nil
}

func (s *session) quit() (Outcome, bool, error) {
	changes := Diff(s.before, s.values)
	if len(changes) == 0 {
		return Outcome{}, true, nil
	}
	discard, err := s.prompter.Confirm(fmt.Sprintf("Discard %d changes?", len(changes)), nil)
	if err != nil && !errors.Is(err, ErrAborted) {
		return Outcome{}, true, err
	}
	return Outcome{}, discard, nil
}
