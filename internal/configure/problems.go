package configure

import "fmt"

func Problems(v Values) []error {
	var problems []error
	if v.Get(PlainFile, "INSTALLATION_NAME") == "" {
		problems = append(problems, fmt.Errorf("%s: INSTALLATION_NAME: can't be empty", PlainFile))
	}
	for _, section := range Sections() {
		for _, f := range section.Fields {
			if err := f.ValidateIn(v); err != nil {
				problems = append(problems, fmt.Errorf("%s: %w", f.File, err))
			}
		}
	}
	return problems
}
