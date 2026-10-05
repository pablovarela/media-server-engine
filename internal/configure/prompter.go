package configure

import "errors"

var ErrAborted = errors.New("aborted")

type MenuItem struct{ ID, Title, Summary string }

type Form struct {
	Values  map[string]string
	Stored  map[string]bool
	Problem string
}

type Prompter interface {
	Menu(title string, items []MenuItem) (string, error)
	Section(title string, fields []Field, form Form) (map[string]string, error)
	Rotate(apps []App) ([]App, error)
	Confirm(question string, lines []string) (bool, error)
}
