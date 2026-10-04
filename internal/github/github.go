package github

type Asset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Release struct {
	Tag        string  `json:"tag_name"`
	URL        string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}
