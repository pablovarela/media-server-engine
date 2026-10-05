package wiring

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

const (
	webConfHeader    = "{\n    \"file\": 2,\n    \"format\": 1\n}"
	pluginConfHeader = "{\n    \"file\": 1,\n    \"format\": 1\n}"
)

type pythonNumber string

func (n pythonNumber) MarshalJSON() ([]byte, error) { return []byte(n), nil }

func (n pythonNumber) String() string { return string(n) }

type pythonFloat float64

func (f pythonFloat) String() string {
	spelled := strconv.FormatFloat(float64(f), 'f', -1, 64)
	if !strings.ContainsAny(spelled, ".eE") {
		spelled += ".0"
	}
	return spelled
}

func (f pythonFloat) MarshalJSON() ([]byte, error) { return []byte(f.String()), nil }

func pythonValue(value any) any {
	switch v := value.(type) {
	case float64:
		return pythonFloat(v)
	case []any:
		converted := make([]any, len(v))
		for n, item := range v {
			converted[n] = pythonValue(item)
		}
		return converted
	case map[string]any:
		converted := make(map[string]any, len(v))
		for key, item := range v {
			converted[key] = pythonValue(item)
		}
		return converted
	}
	return value
}

func readValue(value any) any {
	switch v := value.(type) {
	case json.Number:
		return pythonNumber(v)
	case []any:
		for n, item := range v {
			v[n] = readValue(item)
		}
	case map[string]any:
		for key, item := range v {
			v[key] = readValue(item)
		}
	}
	return value
}

type delugeConf struct {
	path   string
	header []byte
	body   map[string]any
	dirty  bool
}

func readDelugeConf(path, defaultHeader string) (*delugeConf, error) {
	conf := &delugeConf{path: path, header: []byte(defaultHeader), body: map[string]any{}}
	content, err := os.ReadFile(path) //nolint:gosec // Deluge's own config
	if errors.Is(err, os.ErrNotExist) {
		return conf, nil
	}
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var header json.RawMessage
	if err := decoder.Decode(&header); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&conf.body); err != nil {
		return nil, err
	}
	conf.header = header
	readValue(conf.body)
	return conf, nil
}

func (c *delugeConf) set(key string, value any) {
	c.body[key] = value
	c.dirty = true
}

func (c *delugeConf) save() error {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "    ")
	if err := encoder.Encode(c.body); err != nil {
		return err
	}
	content := append(append([]byte{}, c.header...), bytes.TrimRight(body.Bytes(), "\n")...)
	file, err := os.OpenFile(c.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return unwrapPath(err)
	}
	_, err = file.Write(content)
	if closed := file.Close(); err == nil {
		err = closed
	}
	return unwrapPath(err)
}

func unwrapPath(err error) error {
	var failed *fs.PathError
	if errors.As(err, &failed) {
		return failed.Err
	}
	return err
}
