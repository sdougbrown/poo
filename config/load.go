package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

// Format is a settings file's format.
type Format string

const (
	// FormatJSON is the JSON format.
	FormatJSON Format = "json"
	// FormatTOML is the TOML format.
	FormatTOML Format = "toml"
	// FormatYAML is the YAML format.
	FormatYAML Format = "yaml"
)

// FormatFor returns the format a settings file's name says.
//
// The extension decides: .toml is TOML, .yaml and .yml are YAML, and
// everything else is JSON. The settings are JSON first, so a name
// without an extension is JSON.
func FormatFor(path string) Format {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		return FormatTOML
	case ".yaml", ".yml":
		return FormatYAML
	default:
		return FormatJSON
	}
}

// Load reads the settings file at path, decodes it and checks it.
//
// The file's name says its format. A file that does not decode, or
// that holds a key the settings do not have, is a failure.
func Load(path string) (*Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}
	settings, err := Parse(data, FormatFor(path))
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	return settings, nil
}

// Parse decodes the settings in the given format and checks them.
//
// The document is normalized to JSON and decoded strictly: an unknown
// key is an error, and so is a key of the wrong shape.
func Parse(data []byte, format Format) (*Settings, error) {
	normalized, err := Normalize(data, format)
	if err != nil {
		return nil, err
	}
	var settings Settings
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return nil, decodeError(err)
	}
	settings.Permission.askersNamed = namesAskers(normalized)
	if err := settings.Check(); err != nil {
		return nil, err
	}
	return &settings, nil
}

// namesAskers reports whether the document names the asker chain, even
// an empty one.
//
// The settings hold a list, and an empty list decodes the same as an
// absent one: the document is read a second time for the key's
// presence, and the first read is the values.
func namesAskers(document []byte) bool {
	var named struct {
		Permission struct {
			Askers *[]string `json:"askers"`
		} `json:"permission"`
	}
	if err := json.Unmarshal(document, &named); err != nil {
		return false
	}
	return named.Permission.Askers != nil
}

// unknownField matches the field a strict decode did not recognize.
var unknownField = regexp.MustCompile(`unknown field "([^"]+)"`)

// decodeError turns a decode failure into a settings failure.
//
// A key the settings do not have is a failure of its own: the field is
// named, so the operator knows which key to remove.
func decodeError(err error) error {
	if match := unknownField.FindStringSubmatch(err.Error()); match != nil {
		return &Error{
			Code:    CodeUnknownField,
			Field:   match[1],
			Message: "the settings do not have this key",
			Err:     err,
		}
	}
	return &Error{Code: CodeDecode, Message: "the settings do not decode", Err: err}
}

// Normalize converts a settings document to JSON.
//
// The three formats hold the same settings, so they normalize to the
// same JSON: the keys are sorted, and a number is a number whatever
// format it came in.
func Normalize(data []byte, format Format) ([]byte, error) {
	var value any
	switch format {
	case FormatTOML:
		if err := toml.Unmarshal(data, &value); err != nil {
			return nil, &Error{Code: CodeDecode, Message: "the settings are not TOML", Err: err}
		}
	case FormatYAML:
		if err := yaml.Unmarshal(data, &value); err != nil {
			return nil, &Error{Code: CodeDecode, Message: "the settings are not YAML", Err: err}
		}
	case FormatJSON:
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, &Error{Code: CodeDecode, Message: "the settings are not JSON", Err: err}
		}
	default:
		return nil, &Error{Code: CodeDecode, Message: fmt.Sprintf("the settings format %q is not known", format)}
	}
	canonical, err := canonicalValue(value)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(canonical)
	if err != nil {
		return nil, &Error{Code: CodeDecode, Message: "the settings do not normalize to JSON", Err: err}
	}
	return out, nil
}

// canonicalValue turns a decoded document into the value the JSON
// encoder writes.
//
// The numbers are the numbers: an integer in one format and the same
// integer in another are the same value, and the JSON is the same
// either way. A value the settings do not use is a failure: a
// document that holds one does not describe a configuration.
func canonicalValue(value any) (any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case bool:
		return v, nil
	case string:
		return v, nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case time.Time:
		// A date and a time are not settings values. The text is kept
		// so the JSON is well formed, and the strict decode refuses
		// the key it lands in.
		return v.Format(time.RFC3339Nano), nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			canonical, err := canonicalValue(item)
			if err != nil {
				return nil, err
			}
			out[key] = canonical
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			canonical, err := canonicalValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, canonical)
		}
		return out, nil
	default:
		return nil, &Error{
			Code:    CodeDecode,
			Message: fmt.Sprintf("the settings hold a value of a type the settings do not use (%T)", value),
		}
	}
}
