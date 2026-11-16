package gnosis

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Case is a loaded case: a name and steps in file order.
type Case struct {
	Name  string
	Steps []Step
}

// Step is one tap step. Name is empty when the file omits it.
type Step struct {
	Name    string
	Locator Locator
}

// Locator is the set of conditions written on a tap step.
// Index is the 1-based match ordinal. Zero means the file did not set an index.
type Locator struct {
	ID          string
	Text        string
	Description string
	Type        string
	Index       int
}

// Rejection is a load failure. It names the file and the position that could not be read.
type Rejection struct {
	File   string
	Line   int
	Column int
	Reason string
}

func (r *Rejection) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", r.File, r.Line, r.Column, r.Reason)
}

// Load reads the case file at path. It does not modify the file.
func Load(path string) (Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Case{}, &Rejection{File: path, Line: 0, Column: 0, Reason: "cannot read file"}
	}
	if !utf8.Valid(data) {
		line, column := invalidUTF8Position(data)
		return Case{}, &Rejection{
			File:   path,
			Line:   line,
			Column: column,
			Reason: "file is not valid UTF-8",
		}
	}
	doc, err := parseDocument(path, data)
	if err != nil {
		return Case{}, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return Case{}, &Rejection{File: path, Line: 1, Column: 1, Reason: "case must be a mapping"}
	}
	return decodeCase(path, doc.Content[0])
}

func parseDocument(path string, data []byte) (yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if err == io.EOF {
			return yaml.Node{}, &Rejection{File: path, Line: 1, Column: 1, Reason: "case must be a mapping"}
		}
		return yaml.Node{}, rejectionFromYAML(path, err)
	}
	var extra yaml.Node
	err := dec.Decode(&extra)
	if err == io.EOF {
		return doc, nil
	}
	if err != nil {
		return yaml.Node{}, rejectionFromYAML(path, err)
	}
	at := &extra
	if extra.Kind == yaml.DocumentNode && len(extra.Content) > 0 {
		at = extra.Content[0]
	}
	if at.Line == 0 {
		at.Line = 1
		at.Column = 1
	}
	return yaml.Node{}, reject(path, at, "file has more than one document")
}

func decodeCase(path string, node *yaml.Node) (Case, error) {
	if node.Kind != yaml.MappingNode {
		return Case{}, reject(path, node, "case must be a mapping")
	}
	var (
		nameSet bool
		name    string
		steps   *yaml.Node
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, key); err != nil {
			return Case{}, err
		}
		switch key.Value {
		case "name":
			decoded, err := decodeString(path, key, val)
			if err != nil {
				return Case{}, err
			}
			name = decoded
			nameSet = true
		case "steps":
			steps = val
		default:
			return Case{}, reject(path, key, fmt.Sprintf("unknown field %q", key.Value))
		}
	}
	if !nameSet {
		name = nameFromPath(path)
	}
	loaded := Case{Name: name, Steps: []Step{}}
	if steps == nil {
		return loaded, nil
	}
	if steps.Kind != yaml.SequenceNode {
		return Case{}, reject(path, steps, "steps must be a sequence")
	}
	for _, item := range steps.Content {
		step, err := decodeStep(path, item)
		if err != nil {
			return Case{}, err
		}
		loaded.Steps = append(loaded.Steps, step)
	}
	return loaded, nil
}

func decodeStep(path string, node *yaml.Node) (Step, error) {
	if node.Kind != yaml.MappingNode {
		return Step{}, reject(path, node, "step must be a mapping")
	}
	var (
		step   Step
		tap    *yaml.Node
		tapKey *yaml.Node
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, key); err != nil {
			return Step{}, err
		}
		switch key.Value {
		case "name":
			decoded, err := decodeString(path, key, val)
			if err != nil {
				return Step{}, err
			}
			step.Name = decoded
		case "tap":
			tap = val
			tapKey = key
		default:
			return Step{}, reject(path, key, fmt.Sprintf("unknown field %q", key.Value))
		}
	}
	if tap == nil {
		return Step{}, reject(path, node, "missing tap")
	}
	locator, err := decodeTap(path, tapKey, tap)
	if err != nil {
		return Step{}, err
	}
	step.Locator = locator
	return step, nil
}

func decodeTap(path string, key, node *yaml.Node) (Locator, error) {
	if node.Kind != yaml.MappingNode {
		return Locator{}, reject(path, node, "tap must be a mapping")
	}
	var (
		locator    *yaml.Node
		locatorKey *yaml.Node
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return Locator{}, err
		}
		switch field.Value {
		case "locator":
			locator = val
			locatorKey = field
		default:
			return Locator{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
	}
	if locator == nil {
		return Locator{}, reject(path, key, "missing locator")
	}
	return decodeLocator(path, locatorKey, locator)
}

func decodeLocator(path string, key, node *yaml.Node) (Locator, error) {
	if node.Kind != yaml.MappingNode {
		return Locator{}, reject(path, node, "locator must be a mapping")
	}
	var (
		loc          Locator
		hasCondition bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return Locator{}, err
		}
		var err error
		switch field.Value {
		case "id":
			loc.ID, err = decodeString(path, field, val)
			hasCondition = true
		case "text":
			loc.Text, err = decodeString(path, field, val)
			hasCondition = true
		case "description":
			loc.Description, err = decodeString(path, field, val)
			hasCondition = true
		case "type":
			loc.Type, err = decodeString(path, field, val)
			hasCondition = true
		case "index":
			if val.Tag != "!!int" {
				return Locator{}, reject(path, val, `field "index" must be an integer`)
			}
			var n int
			n, err = strconv.Atoi(val.Value)
			if err != nil {
				return Locator{}, reject(path, val, `field "index" must be an integer`)
			}
			if n < 1 {
				return Locator{}, reject(path, val, "index must be >= 1")
			}
			loc.Index = n
		default:
			return Locator{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
		if err != nil {
			return Locator{}, err
		}
	}
	if !hasCondition {
		return Locator{}, reject(path, key, "locator requires id, text, description, or type")
	}
	return loc, nil
}

func decodeString(path string, key, val *yaml.Node) (string, error) {
	if val.Tag != "!!str" {
		return "", reject(path, val, fmt.Sprintf("field %q must be a string", key.Value))
	}
	return val.Value, nil
}

func noteKey(path string, seen map[string]bool, key *yaml.Node) error {
	if seen[key.Value] {
		return reject(path, key, fmt.Sprintf("duplicate key %q", key.Value))
	}
	seen[key.Value] = true
	return nil
}

func reject(path string, node *yaml.Node, reason string) error {
	return &Rejection{File: path, Line: node.Line, Column: node.Column, Reason: reason}
}

func rejectionFromYAML(path string, err error) error {
	reason := strings.TrimPrefix(err.Error(), "yaml: ")
	line := 1
	if rest, ok := strings.CutPrefix(reason, "line "); ok {
		number, after, found := strings.Cut(rest, ": ")
		if parsed, convErr := strconv.Atoi(number); found && convErr == nil {
			line = parsed
			reason = after
		}
	}
	return &Rejection{File: path, Line: line, Column: 1, Reason: reason}
}

func invalidUTF8Position(data []byte) (int, int) {
	line, column := 1, 1
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size == 1 {
			return line, column
		}
		if r == '\n' {
			line++
			column = 1
		} else {
			column++
		}
		i += size
	}
	return line, column
}

func nameFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
