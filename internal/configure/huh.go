package configure

import (
	"context"
	"errors"
	"strings"

	"charm.land/huh/v2"
)

const keepsTheCurrentValue = "unchanged (Enter keeps it)"

type Huh struct {
	Ctx context.Context
}

func (h Huh) Menu(title string, items []MenuItem) (string, error) {
	var choice string
	options := make([]huh.Option[string], 0, len(items))
	for _, item := range items {
		label := item.Title
		if item.Summary != "" {
			label += "  " + item.Summary
		}
		options = append(options, huh.NewOption(label, item.ID))
	}
	err := h.run(huh.NewGroup(huh.NewSelect[string]().Title(title).Options(options...).Value(&choice)))
	return choice, err
}

func (h Huh) Section(title string, fields []Field, current map[string]string, problem string) (map[string]string, error) {
	entered := make(map[string]*string, len(fields))
	inputs := make([]huh.Field, 0, len(fields)+1)
	if problem != "" {
		inputs = append(inputs, huh.NewNote().Title(problem))
	}
	for _, f := range fields {
		inputs = append(inputs, input(f, current[f.Key], entered))
	}
	if err := h.run(huh.NewGroup(inputs...).Title(title)); err != nil {
		return nil, err
	}
	answer := make(map[string]string, len(fields))
	for key, value := range entered {
		answer[key] = strings.TrimSpace(*value)
	}
	return answer, nil
}

func input(f Field, current string, entered map[string]*string) *huh.Input {
	value := current
	field := huh.NewInput().Title(f.Title).Description(f.Key)
	validate := f.Validate
	if f.Masked {
		value = ""
		field = field.EchoMode(huh.EchoModePassword)
		if current != "" {
			field = field.Placeholder(keepsTheCurrentValue)
			validate = func(v string) error {
				if v == "" {
					return nil
				}
				return f.Validate(v)
			}
		}
	}
	entered[f.Key] = &value
	return field.Value(&value).Validate(func(v string) error { return validate(strings.TrimSpace(v)) })
}

func (h Huh) Rotate(apps []App) ([]App, error) {
	var chosen []string
	options := make([]huh.Option[string], 0, len(apps))
	for _, app := range apps {
		options = append(options, huh.NewOption(app.Name, app.Key))
	}
	err := h.run(huh.NewGroup(huh.NewMultiSelect[string]().
		Title("Rotate API keys").
		Description("Each one picked gets a new random key; the apps take it at the next apply.").
		Options(options...).Value(&chosen)))
	var picked []App
	for _, app := range apps {
		for _, key := range chosen {
			if key == app.Key {
				picked = append(picked, app)
			}
		}
	}
	return picked, err
}

func (h Huh) Confirm(question string, lines []string) (bool, error) {
	confirmed := false
	err := h.run(huh.NewGroup(huh.NewConfirm().Title(question).Description(strings.Join(lines, "\n")).
		Affirmative("Yes").Negative("No").Value(&confirmed)))
	return confirmed, err
}

func (h Huh) run(group *huh.Group) error {
	err := huh.NewForm(group).RunWithContext(h.Ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}
