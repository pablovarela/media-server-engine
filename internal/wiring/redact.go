package wiring

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const shortestSecret = 6

var (
	secretField  = regexp.MustCompile(`(?i)key|password|token|secret`)
	quotedSecret = regexp.MustCompile(`"([^"]{6,})"`)
)

type Redactor struct {
	mu    sync.Mutex
	known map[string]bool
}

func NewRedactor(secrets map[string]string) *Redactor {
	r := &Redactor{known: map[string]bool{}}
	for _, secret := range secrets {
		r.add(secret)
	}
	return r
}

func (r *Redactor) add(secret string) {
	if len(secret) >= shortestSecret {
		r.known[secret] = true
	}
}

func (r *Redactor) Sent(headers map[string]string, body any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range headers {
		r.add(value)
		for _, quoted := range quotedSecret.FindAllStringSubmatch(value, -1) {
			r.add(quoted[1])
		}
	}
	if form, ok := body.(url.Values); ok {
		for key, values := range form {
			if secretField.MatchString(key) {
				for _, value := range values {
					r.add(value)
				}
			}
		}
		return
	}
	if body == nil {
		return
	}
	var generic any
	if encoded, err := json.Marshal(body); err == nil && json.Unmarshal(encoded, &generic) == nil {
		r.walk(generic)
	}
}

func (r *Redactor) walk(item any) {
	switch value := item.(type) {
	case map[string]any:
		for key, inner := range value {
			if text, ok := inner.(string); ok && secretField.MatchString(key) {
				r.add(text)
				continue
			}
			r.walk(inner)
		}
	case []any:
		for _, inner := range value {
			r.walk(inner)
		}
	}
}

func (r *Redactor) Hide(text string) string {
	r.mu.Lock()
	secrets := make([]string, 0, len(r.known))
	for secret := range r.known {
		secrets = append(secrets, secret)
	}
	r.mu.Unlock()
	sort.Slice(secrets, func(a, b int) bool { return len(secrets[a]) > len(secrets[b]) })
	for _, secret := range secrets {
		text = replaceWhole(text, secret)
	}
	return text
}

func replaceWhole(text, secret string) string {
	var out strings.Builder
	for {
		at := strings.Index(text, secret)
		if at < 0 {
			out.WriteString(text)
			return out.String()
		}
		end := at + len(secret)
		if wordBefore(text, at) || wordAfter(text, end) {
			out.WriteString(text[:end])
		} else {
			out.WriteString(text[:at] + "<hidden>")
		}
		text = text[end:]
	}
}

func wordBefore(text string, at int) bool {
	return at > 0 && isWord(rune(text[at-1]))
}

func wordAfter(text string, end int) bool {
	return end < len(text) && isWord(rune(text[end]))
}

func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
