package gnosis

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type modelDecision struct {
	stop   bool
	action Step
	row    *yaml.Node
	note   string
}

var errModelDecision = errors.New("AI model returned an invalid decision")

const maxModelNoteRunes = 512

func decodeModelDecision(content, secret string) (modelDecision, error) {
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	object, err := readDecisionNode(decoder, secret, 0)
	if err != nil || object.Kind != yaml.MappingNode {
		return modelDecision{}, errModelDecision
	}
	if _, err := decoder.Token(); err != io.EOF {
		return modelDecision{}, errModelDecision
	}

	var stop, action, params, note *yaml.Node
	for i := 0; i < len(object.Content); i += 2 {
		value := object.Content[i+1]
		switch object.Content[i].Value {
		case "stop":
			stop = value
		case "action":
			action = value
		case "params":
			params = value
		case "note":
			note = value
		default:
			return modelDecision{}, errModelDecision
		}
	}
	if stop != nil {
		if len(object.Content) == 2 && stop.Tag == "!!bool" && stop.Value == "true" {
			return modelDecision{stop: true}, nil
		}
		return modelDecision{}, errModelDecision
	}
	if action == nil || action.Tag != "!!str" || params == nil || params.Kind != yaml.MappingNode ||
		note == nil || note.Tag != "!!str" || strings.TrimSpace(note.Value) == "" ||
		utf8.RuneCountInString(note.Value) > maxModelNoteRunes {
		return modelDecision{}, errModelDecision
	}
	switch action.Value {
	case "launch", "tap", "long_press", "tap_at", "swipe", "type", "key", "wait":
	default:
		return modelDecision{}, errors.New("AI model requested an unsupported action")
	}

	// Feed the validated object to the same action decoder as a case file.
	// Its raw errors may quote model inputs, so report only the safe category.
	row := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: []*yaml.Node{action, params},
	}
	step, err := decodeStep("", row)
	if err != nil {
		return modelDecision{}, errors.New("AI model returned invalid action parameters")
	}
	return modelDecision{action: step, row: row, note: note.Value}, nil
}

// readDecisionNode keeps exact JSON scalar types when passing the decision
// to the shared action decoder. It rejects duplicate fields, arrays and null.
func readDecisionNode(decoder *json.Decoder, secret string, depth int) (*yaml.Node, error) {
	// Scalar fields of params.locator.relative.anchor are at depth five.
	if depth > 5 {
		return nil, errModelDecision
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, errModelDecision
	}
	switch value := token.(type) {
	case json.Delim:
		if value != '{' {
			return nil, errModelDecision
		}
		object := &yaml.Node{Kind: yaml.MappingNode}
		seen := make(map[string]bool)
		for decoder.More() {
			nameToken, err := decoder.Token()
			name, ok := nameToken.(string)
			if err != nil || !ok || seen[name] {
				return nil, errModelDecision
			}
			seen[name] = true
			child, err := readDecisionNode(decoder, secret, depth+1)
			if err != nil {
				return nil, err
			}
			object.Content = append(object.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, child)
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
			return nil, errModelDecision
		}
		return object, nil
	case string:
		// Check decoded values so JSON escapes cannot conceal the API key.
		if secret != "" && strings.Contains(value, secret) {
			return nil, errModelDecision
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}, nil
	case json.Number:
		number := value.String()
		tag := "!!int"
		if strings.ContainsAny(number, ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: number}, nil
	case bool:
		if value {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}, nil
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}, nil
	default:
		return nil, errModelDecision
	}
}
