package configure

func Problems(v Values) []error {
	var problems []error
	for _, section := range Sections() {
		for _, f := range section.Fields {
			if err := f.ValidateIn(v); err != nil {
				problems = append(problems, err)
			}
		}
	}
	return problems
}
