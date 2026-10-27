package configure

import "fmt"

type Sealed map[string]map[string]bool

func (s Sealed) Has(file, key string) bool {
	return s[file][key]
}

func Problems(v Values, sealed Sealed) []error {
	var problems []error
	if v.Get(PlainFile, "INSTALLATION_NAME") == "" {
		problems = append(problems, fmt.Errorf("%s: INSTALLATION_NAME: can't be empty", PlainFile))
	}
	for _, section := range Sections() {
		for _, f := range section.Fields {
			if sealed.Has(f.File, f.Key) {
				continue
			}
			if err := f.ValidateIn(v); err != nil {
				problems = append(problems, fmt.Errorf("%s: %w", f.File, err))
			}
		}
	}
	return problems
}
