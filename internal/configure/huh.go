package configure

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
)

const keepsTheCurrentValue = "unchanged (Enter keeps it"

type Huh struct {
	Ctx context.Context
}

func (h Huh) Menu(title string, items []MenuItem) (string, error) {
	var choice string
	options := make([]huh.Option[string], 0, len(items))
	for _, item := range items {
		options = append(options, huh.NewOption(menuLabel(item), item.ID))
	}
	err := h.run(huh.NewGroup(huh.NewSelect[string]().Title(title).Options(options...).Value(&choice)))
	return choice, err
}

func (h Huh) Section(title string, fields []Field, form Form) (map[string]string, error) {
	entered := make(map[string]*string, len(fields))
	inputs := make([]huh.Field, 0, len(fields)+1)
	if form.Problem != "" {
		inputs = append(inputs, huh.NewNote().Title(form.Problem))
	}
	for _, f := range fields {
		inputs = append(inputs, input(f, form, entered))
	}
	if err := h.run(huh.NewGroup(inputs...).Title(title)); err != nil {
		return nil, err
	}
	answer := make(map[string]string, len(fields))
	for _, f := range fields {
		answer[f.Key] = answered(f, *entered[f.Key])
	}
	return answer, nil
}

func input(f Field, form Form, entered map[string]*string) *huh.Input {
	value, placeholder := shown(f, form)
	entered[f.Key] = &value
	field := huh.NewInput().Title(f.Title).Description(f.Key).Placeholder(placeholder)
	if f.Masked {
		field = field.EchoMode(huh.EchoModePassword)
	}
	return field.Value(&value).Validate(func(v string) error {
		v = answered(f, v)
		if f.Masked && (v == "" && form.Stored[f.Key] || v == removeSecret && f.Optional) {
			return nil
		}
		return f.Validate(v)
	})
}

func shown(f Field, form Form) (value, placeholder string) {
	value = form.Values[f.Key]
	if !f.Masked || !form.Stored[f.Key] {
		return value, ""
	}
	if f.Optional {
		return value, keepsTheCurrentValue + ", " + removeSecret + " removes it)"
	}
	return value, keepsTheCurrentValue + ")"
}

func menuLabel(item MenuItem) string {
	return strings.TrimRight(fmt.Sprintf("%-14s%s", item.Title, item.Summary), " ")
}

func answered(f Field, value string) string {
	if f.Masked {
		return value
	}
	return strings.TrimSpace(value)
}

func keys() *huh.KeyMap {
	keyMap := huh.NewDefaultKeyMap()
	keyMap.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"))
	return keyMap
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
	err := huh.NewForm(group).WithKeyMap(keys()).RunWithContext(h.Ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

func (h Huh) Acknowledge(title, text, word string) error {
	var typed string
	return h.run(huh.NewGroup(
		huh.NewNote().Title(title).Description(text),
		huh.NewInput().Title("Type "+word+" once it is saved").Value(&typed).Validate(func(v string) error {
			if strings.TrimSpace(v) != word {
				return fmt.Errorf("type %s to go on, or esc to stop", word)
			}
			return nil
		}),
	))
}
