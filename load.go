package gnosis

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Case is a loaded case: a name and steps in file order.
type Case struct {
	Name  string
	Steps []Step
}

// Step is one action, assertion, or AI step. Name is empty when the file omits it.
// A tap stores its locator in Locator. Every other action leaves Locator zero
// and sets the field for that action. An assertion sets Precondition or Expect.
// An AI step sets AI.
type Step struct {
	Name         string
	Locator      Locator
	Launch       *Launch
	LongPress    *LongPress
	TapAt        *TapAt
	Swipe        *Swipe
	Type         *Type
	Key          *Key
	Wait         *Wait
	Precondition *Assertion
	Expect       *Assertion
	AI           *AI
}

// AI is one AI step. Goal is the outcome to reach on the device.
// MaxSteps is 20 when the file omits it.
type AI struct {
	Goal     string
	MaxSteps int
}

// Assertion is the body of a precondition or expect step.
// Timeout is 10s when the file omits it. Exactly one check is set.
type Assertion struct {
	Timeout      time.Duration
	Exists       *Locator
	Absent       *Locator
	TextEquals   *TextCheck
	TextContains *TextCheck
	AttrEquals   *AttrEquals
	ScreenText   *ScreenText
}

// ScreenText is text read from the pixels of the screen. It has no locator.
type ScreenText struct {
	Text string
}

// AttrEquals compares an element's enabled or checked attribute with Value.
type AttrEquals struct {
	Locator   Locator
	Attribute string
	Value     bool
}

// TextCheck compares an element's display text with Text.
type TextCheck struct {
	Locator Locator
	Text    string
}

// Launch starts an application. Entry is empty when the file omits it.
type Launch struct {
	App   string
	Entry string
}

// LongPress is a long press on a locator.
// Duration is 800ms when the file omits it.
type LongPress struct {
	Locator  Locator
	Duration time.Duration
}

// TapAt is a tap at fractions of the screen width and height.
// X and Y are kept even when they fall outside 0..1.
type TapAt struct {
	X float64
	Y float64
}

// Swipe moves between two points given as fractions of the screen width and height.
// The fractions are kept even when they fall outside 0..1.
// Duration is 300ms when the file omits it.
type Swipe struct {
	FromX    float64
	FromY    float64
	ToX      float64
	ToY      float64
	Duration time.Duration
}

// Type enters text into the focused field.
type Type struct {
	Text string
}

// Key presses back or home.
type Key struct {
	Name string
}

// Wait pauses for Duration. The file must set it; there is no default.
type Wait struct {
	Duration time.Duration
}

// Locator is the set of conditions that find an element.
// A nil string condition is omitted; a pointer to "" is an explicit empty condition.
// Index is the 1-based match ordinal. Zero means the file did not set an index.
type Locator struct {
	ID          *string
	Text        *string
	Description *string
	Type        *string
	Index       int
}

// Suite is a loaded suite: cases in file order. A suite has no name.
type Suite struct {
	Cases []Case
}

// Rejection is a load failure. It names the file and the position that could not be read.
// Reference is the 1-based suite entry when a referenced case failed. CaseFile, CaseLine,
// and CaseColumn name that case. They are zero when the reference itself could not be read.
type Rejection struct {
	File       string
	Line       int
	Column     int
	Reason     string
	Reference  int
	CaseFile   string
	CaseLine   int
	CaseColumn int
}

func (r *Rejection) Error() string {
	if r.Reference > 0 {
		return fmt.Sprintf("%s:%d:%d: reference %d (%s:%d:%d): %s", r.File, r.Line, r.Column, r.Reference, r.CaseFile, r.CaseLine, r.CaseColumn, r.Reason)
	}
	return fmt.Sprintf("%s:%d:%d: %s", r.File, r.Line, r.Column, r.Reason)
}

// Load reads the case or suite file at path. A file with cases is a suite.
// A file with name or steps is a case. It does not modify any file.
// The suite result is nil when path is a case. The case result is zero when path is a suite.
func Load(path string) (Case, *Suite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Case{}, nil, &Rejection{File: path, Line: 0, Column: 0, Reason: "cannot read file"}
	}
	node, err := parseFile(path, data)
	if err != nil {
		return Case{}, nil, err
	}
	return decodeFile(path, node)
}

// parseFile validates the document without following any suite references.
func parseFile(path string, data []byte) (*yaml.Node, error) {
	if !utf8.Valid(data) {
		line, column := invalidUTF8Position(data)
		return nil, &Rejection{
			File:   path,
			Line:   line,
			Column: column,
			Reason: "file is not valid UTF-8",
		}
	}
	doc, err := parseDocument(path, data)
	if err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, &Rejection{File: path, Line: 1, Column: 1, Reason: "case must be a mapping"}
	}
	node := doc.Content[0]
	if node.Kind != yaml.MappingNode {
		return nil, reject(path, node, "case must be a mapping")
	}
	return node, nil
}

func decodeFile(path string, node *yaml.Node) (Case, *Suite, error) {
	var hasCases, hasCase bool
	for i := 0; i < len(node.Content); i += 2 {
		switch node.Content[i].Value {
		case "cases":
			hasCases = true
		case "name", "steps":
			hasCase = true
		}
	}
	if hasCases && hasCase {
		return Case{}, nil, rejectMixed(path, node)
	}
	if hasCases {
		suite, err := decodeSuite(path, node)
		return Case{}, suite, err
	}
	loaded, err := decodeCase(path, node)
	return loaded, nil, err
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
		step      Step
		action    string
		actionKey *yaml.Node
		actionVal *yaml.Node
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
		case "tap", "launch", "long_press", "tap_at", "swipe", "type", "key", "wait", "precondition", "expect", "ai":
			if action != "" {
				return Step{}, reject(path, key, "step has more than one action")
			}
			action = key.Value
			actionKey = key
			actionVal = val
		default:
			return Step{}, reject(path, key, fmt.Sprintf("unknown field %q", key.Value))
		}
	}
	switch action {
	case "tap":
		locator, err := decodeTap(path, actionKey, actionVal)
		if err != nil {
			return Step{}, err
		}
		step.Locator = locator
	case "launch":
		launch, err := decodeLaunch(path, actionKey, actionVal)
		if err != nil {
			return Step{}, err
		}
		step.Launch = &launch
	case "long_press":
		press, err := decodeLongPress(path, actionKey, actionVal)
		if err != nil {
			return Step{}, err
		}
		step.LongPress = &press
	case "tap_at":
		point, err := decodeTapAt(path, actionKey, actionVal)
		if err != nil {
			return Step{}, err
		}
		step.TapAt = &point
	case "swipe":
		swipe, err := decodeSwipe(path, actionKey, actionVal)
		if err != nil {
			return Step{}, err
		}
		step.Swipe = &swipe
	case "type":
		typed, err := decodeType(path, actionKey, actionVal)
		if err != nil {
			return Step{}, err
		}
		step.Type = &typed
	case "key":
		keyPress, err := decodeKey(path, actionKey, actionVal)
		if err != nil {
			return Step{}, err
		}
		step.Key = &keyPress
	case "wait":
		pause, err := decodeWait(path, actionKey, actionVal)
		if err != nil {
			return Step{}, err
		}
		step.Wait = &pause
	case "precondition":
		assertion, err := decodeAssertion(path, action, actionKey, actionVal)
		if err != nil {
			return Step{}, err
		}
		step.Precondition = &assertion
	case "expect":
		assertion, err := decodeAssertion(path, action, actionKey, actionVal)
		if err != nil {
			return Step{}, err
		}
		step.Expect = &assertion
	case "ai":
		ai, err := decodeAI(path, actionKey, actionVal)
		if err != nil {
			return Step{}, err
		}
		step.AI = &ai
	default:
		return Step{}, reject(path, node, "missing tap")
	}
	return step, nil
}

func decodeAI(path string, key, node *yaml.Node) (AI, error) {
	if node.Kind != yaml.MappingNode {
		return AI{}, reject(path, node, "ai must be a mapping")
	}
	var (
		ai      AI
		hasGoal bool
		hasMax  bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return AI{}, err
		}
		switch field.Value {
		case "goal":
			decoded, err := decodeString(path, field, val)
			if err != nil {
				return AI{}, err
			}
			ai.Goal = decoded
			hasGoal = true
		case "max_steps":
			decoded, err := decodeMaxSteps(path, val)
			if err != nil {
				return AI{}, err
			}
			ai.MaxSteps = decoded
			hasMax = true
		default:
			return AI{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
	}
	if !hasGoal {
		return AI{}, reject(path, key, "missing goal")
	}
	if !hasMax {
		ai.MaxSteps = 20
	}
	return ai, nil
}

func decodeMaxSteps(path string, node *yaml.Node) (int, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
		return 0, reject(path, node, `field "max_steps" must be a positive integer`)
	}
	var n int
	err := node.Decode(&n)
	if err != nil || n < 1 {
		return 0, reject(path, node, `field "max_steps" must be a positive integer`)
	}
	return n, nil
}

func decodeAssertion(path, kind string, key, node *yaml.Node) (Assertion, error) {
	if node.Kind != yaml.MappingNode {
		return Assertion{}, reject(path, node, kind+" must be a mapping")
	}
	var (
		assertion  Assertion
		hasTimeout bool
		hasCheck   bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return Assertion{}, err
		}
		switch field.Value {
		case "timeout":
			decoded, err := decodeDuration(path, val)
			if err != nil {
				return Assertion{}, err
			}
			assertion.Timeout = decoded
			hasTimeout = true
		case "exists":
			if err := noteCheck(path, &hasCheck, field); err != nil {
				return Assertion{}, err
			}
			locator, err := decodeLocator(path, field, val)
			if err != nil {
				return Assertion{}, err
			}
			assertion.Exists = &locator
		case "absent":
			if err := noteCheck(path, &hasCheck, field); err != nil {
				return Assertion{}, err
			}
			locator, err := decodeLocator(path, field, val)
			if err != nil {
				return Assertion{}, err
			}
			assertion.Absent = &locator
		case "text_equals":
			if err := noteCheck(path, &hasCheck, field); err != nil {
				return Assertion{}, err
			}
			check, err := decodeTextCheck(path, field.Value, field, val)
			if err != nil {
				return Assertion{}, err
			}
			assertion.TextEquals = &check
		case "text_contains":
			if err := noteCheck(path, &hasCheck, field); err != nil {
				return Assertion{}, err
			}
			check, err := decodeTextCheck(path, field.Value, field, val)
			if err != nil {
				return Assertion{}, err
			}
			assertion.TextContains = &check
		case "attr_equals":
			if err := noteCheck(path, &hasCheck, field); err != nil {
				return Assertion{}, err
			}
			check, err := decodeAttrEquals(path, field, val)
			if err != nil {
				return Assertion{}, err
			}
			assertion.AttrEquals = &check
		case "screen_text":
			if err := noteCheck(path, &hasCheck, field); err != nil {
				return Assertion{}, err
			}
			check, err := decodeScreenText(path, field, val)
			if err != nil {
				return Assertion{}, err
			}
			assertion.ScreenText = &check
		default:
			return Assertion{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
	}
	if !hasTimeout {
		assertion.Timeout = 10 * time.Second
	}
	if !hasCheck {
		return Assertion{}, reject(path, key, "missing check")
	}
	return assertion, nil
}

func decodeScreenText(path string, key, node *yaml.Node) (ScreenText, error) {
	text, err := decodeRequiredText(path, "screen_text", key, node)
	return ScreenText{Text: text}, err
}

func decodeRequiredText(path, kind string, key, node *yaml.Node) (string, error) {
	if node.Kind != yaml.MappingNode {
		return "", reject(path, node, kind+" must be a mapping")
	}
	var (
		text    string
		hasText bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return "", err
		}
		switch field.Value {
		case "text":
			decoded, err := decodeString(path, field, val)
			if err != nil {
				return "", err
			}
			text = decoded
			hasText = true
		default:
			return "", reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
	}
	if !hasText {
		return "", reject(path, key, "missing text")
	}
	return text, nil
}

func decodeAttrEquals(path string, key, node *yaml.Node) (AttrEquals, error) {
	if node.Kind != yaml.MappingNode {
		return AttrEquals{}, reject(path, node, "attr_equals must be a mapping")
	}
	var (
		check      AttrEquals
		locator    *yaml.Node
		locatorKey *yaml.Node
		hasAttr    bool
		hasValue   bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return AttrEquals{}, err
		}
		switch field.Value {
		case "locator":
			locator = val
			locatorKey = field
		case "attribute":
			decoded, err := decodeString(path, field, val)
			if err != nil {
				return AttrEquals{}, err
			}
			if decoded != "enabled" && decoded != "checked" {
				return AttrEquals{}, reject(path, val, fmt.Sprintf("unknown attribute %q", decoded))
			}
			check.Attribute = decoded
			hasAttr = true
		case "value":
			decoded, err := decodeBool(path, field, val)
			if err != nil {
				return AttrEquals{}, err
			}
			check.Value = decoded
			hasValue = true
		default:
			return AttrEquals{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
	}
	if locator == nil {
		return AttrEquals{}, reject(path, key, "missing locator")
	}
	if !hasAttr {
		return AttrEquals{}, reject(path, key, "missing attribute")
	}
	if !hasValue {
		return AttrEquals{}, reject(path, key, "missing value")
	}
	decoded, err := decodeLocator(path, locatorKey, locator)
	if err != nil {
		return AttrEquals{}, err
	}
	check.Locator = decoded
	return check, nil
}

func decodeBool(path string, key, val *yaml.Node) (bool, error) {
	if val.Kind != yaml.ScalarNode || val.Tag != "!!bool" {
		return false, reject(path, val, fmt.Sprintf("field %q must be a boolean", key.Value))
	}
	decoded, err := strconv.ParseBool(val.Value)
	if err != nil {
		return false, reject(path, val, fmt.Sprintf("field %q must be a boolean", key.Value))
	}
	return decoded, nil
}

func decodeTextCheck(path, kind string, key, node *yaml.Node) (TextCheck, error) {
	if node.Kind != yaml.MappingNode {
		return TextCheck{}, reject(path, node, kind+" must be a mapping")
	}
	var (
		check      TextCheck
		locator    *yaml.Node
		locatorKey *yaml.Node
		hasText    bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return TextCheck{}, err
		}
		switch field.Value {
		case "locator":
			locator = val
			locatorKey = field
		case "text":
			decoded, err := decodeString(path, field, val)
			if err != nil {
				return TextCheck{}, err
			}
			check.Text = decoded
			hasText = true
		default:
			return TextCheck{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
	}
	if locator == nil {
		return TextCheck{}, reject(path, key, "missing locator")
	}
	if !hasText {
		return TextCheck{}, reject(path, key, "missing text")
	}
	decoded, err := decodeLocator(path, locatorKey, locator)
	if err != nil {
		return TextCheck{}, err
	}
	check.Locator = decoded
	return check, nil
}

func decodeLaunch(path string, key, node *yaml.Node) (Launch, error) {
	if node.Kind != yaml.MappingNode {
		return Launch{}, reject(path, node, "launch must be a mapping")
	}
	var (
		launch Launch
		hasApp bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return Launch{}, err
		}
		var err error
		switch field.Value {
		case "app":
			launch.App, err = decodeString(path, field, val)
			hasApp = true
		case "entry":
			launch.Entry, err = decodeString(path, field, val)
		default:
			return Launch{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
		if err != nil {
			return Launch{}, err
		}
	}
	if !hasApp {
		return Launch{}, reject(path, key, "missing app")
	}
	return launch, nil
}

func decodeTapAt(path string, key, node *yaml.Node) (TapAt, error) {
	if node.Kind != yaml.MappingNode {
		return TapAt{}, reject(path, node, "tap_at must be a mapping")
	}
	var (
		point TapAt
		hasX  bool
		hasY  bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return TapAt{}, err
		}
		var err error
		switch field.Value {
		case "x":
			point.X, err = decodeNumber(path, field, val)
			hasX = true
		case "y":
			point.Y, err = decodeNumber(path, field, val)
			hasY = true
		default:
			return TapAt{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
		if err != nil {
			return TapAt{}, err
		}
	}
	if !hasX {
		return TapAt{}, reject(path, key, "missing x")
	}
	if !hasY {
		return TapAt{}, reject(path, key, "missing y")
	}
	return point, nil
}

func decodeWait(path string, key, node *yaml.Node) (Wait, error) {
	if node.Kind != yaml.MappingNode {
		return Wait{}, reject(path, node, "wait must be a mapping")
	}
	var (
		pause       Wait
		hasDuration bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return Wait{}, err
		}
		switch field.Value {
		case "duration":
			decoded, err := decodeDuration(path, val)
			if err != nil {
				return Wait{}, err
			}
			pause.Duration = decoded
			hasDuration = true
		default:
			return Wait{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
	}
	if !hasDuration {
		return Wait{}, reject(path, key, "missing duration")
	}
	return pause, nil
}

func decodeKey(path string, key, node *yaml.Node) (Key, error) {
	if node.Kind != yaml.MappingNode {
		return Key{}, reject(path, node, "key must be a mapping")
	}
	var (
		press   Key
		hasName bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return Key{}, err
		}
		switch field.Value {
		case "name":
			decoded, err := decodeString(path, field, val)
			if err != nil {
				return Key{}, err
			}
			if decoded != "back" && decoded != "home" {
				return Key{}, reject(path, val, fmt.Sprintf("unknown key %q", decoded))
			}
			press.Name = decoded
			hasName = true
		default:
			return Key{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
	}
	if !hasName {
		return Key{}, reject(path, key, "missing name")
	}
	return press, nil
}

func decodeType(path string, key, node *yaml.Node) (Type, error) {
	text, err := decodeRequiredText(path, "type", key, node)
	return Type{Text: text}, err
}

func decodeSwipe(path string, key, node *yaml.Node) (Swipe, error) {
	if node.Kind != yaml.MappingNode {
		return Swipe{}, reject(path, node, "swipe must be a mapping")
	}
	var (
		swipe       Swipe
		hasFromX    bool
		hasFromY    bool
		hasToX      bool
		hasToY      bool
		hasDuration bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return Swipe{}, err
		}
		var err error
		switch field.Value {
		case "from_x":
			swipe.FromX, err = decodeNumber(path, field, val)
			hasFromX = true
		case "from_y":
			swipe.FromY, err = decodeNumber(path, field, val)
			hasFromY = true
		case "to_x":
			swipe.ToX, err = decodeNumber(path, field, val)
			hasToX = true
		case "to_y":
			swipe.ToY, err = decodeNumber(path, field, val)
			hasToY = true
		case "duration":
			decoded, durErr := decodeDuration(path, val)
			if durErr != nil {
				return Swipe{}, durErr
			}
			swipe.Duration = decoded
			hasDuration = true
		default:
			return Swipe{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
		if err != nil {
			return Swipe{}, err
		}
	}
	switch {
	case !hasFromX:
		return Swipe{}, reject(path, key, "missing from_x")
	case !hasFromY:
		return Swipe{}, reject(path, key, "missing from_y")
	case !hasToX:
		return Swipe{}, reject(path, key, "missing to_x")
	case !hasToY:
		return Swipe{}, reject(path, key, "missing to_y")
	}
	if !hasDuration {
		swipe.Duration = 300 * time.Millisecond
	}
	return swipe, nil
}

func decodeNumber(path string, key, val *yaml.Node) (float64, error) {
	if val.Kind != yaml.ScalarNode || (val.Tag != "!!int" && val.Tag != "!!float") {
		return 0, reject(path, val, fmt.Sprintf("field %q must be a number", key.Value))
	}
	var n float64
	if err := val.Decode(&n); err != nil {
		return 0, reject(path, val, fmt.Sprintf("field %q must be a number", key.Value))
	}
	return n, nil
}

func decodeLongPress(path string, key, node *yaml.Node) (LongPress, error) {
	if node.Kind != yaml.MappingNode {
		return LongPress{}, reject(path, node, "long_press must be a mapping")
	}
	var (
		press       LongPress
		locator     *yaml.Node
		locatorKey  *yaml.Node
		hasDuration bool
	)
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return LongPress{}, err
		}
		switch field.Value {
		case "locator":
			locator = val
			locatorKey = field
		case "duration":
			decoded, err := decodeDuration(path, val)
			if err != nil {
				return LongPress{}, err
			}
			press.Duration = decoded
			hasDuration = true
		default:
			return LongPress{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
	}
	if !hasDuration {
		press.Duration = 800 * time.Millisecond
	}
	if locator == nil {
		return LongPress{}, reject(path, key, "missing locator")
	}
	decoded, err := decodeLocator(path, locatorKey, locator)
	if err != nil {
		return LongPress{}, err
	}
	press.Locator = decoded
	return press, nil
}

func decodeDuration(path string, node *yaml.Node) (time.Duration, error) {
	if node.Kind != yaml.ScalarNode {
		return 0, reject(path, node, "invalid duration")
	}
	decoded, ok := parseDuration(node.Value)
	if !ok {
		return 0, reject(path, node, fmt.Sprintf("invalid duration %q", node.Value))
	}
	return decoded, nil
}

func parseDuration(raw string) (time.Duration, bool) {
	var (
		digits string
		unit   time.Duration
		ok     bool
	)
	if digits, ok = strings.CutSuffix(raw, "ms"); ok {
		unit = time.Millisecond
	} else if digits, ok = strings.CutSuffix(raw, "s"); ok {
		unit = time.Second
	} else {
		return 0, false
	}
	if digits == "" {
		return 0, false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, false
	}
	if n > int64(math.MaxInt64/unit) {
		return 0, false
	}
	return time.Duration(n) * unit, true
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
	var loc Locator
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, field); err != nil {
			return Locator{}, err
		}
		var condition **string
		switch field.Value {
		case "id":
			condition = &loc.ID
		case "text":
			condition = &loc.Text
		case "description":
			condition = &loc.Description
		case "type":
			condition = &loc.Type
		case "index":
			if val.Kind != yaml.ScalarNode || val.Tag != "!!int" {
				return Locator{}, reject(path, val, `field "index" must be an integer`)
			}
			var n int
			if err := val.Decode(&n); err != nil {
				return Locator{}, reject(path, val, `field "index" must be an integer`)
			}
			if n < 1 {
				return Locator{}, reject(path, val, "index must be >= 1")
			}
			loc.Index = n
			continue
		default:
			return Locator{}, reject(path, field, fmt.Sprintf("unknown field %q", field.Value))
		}
		decoded, err := decodeString(path, field, val)
		if err != nil {
			return Locator{}, err
		}
		*condition = &decoded
	}
	if loc.ID == nil && loc.Text == nil && loc.Description == nil && loc.Type == nil {
		return Locator{}, reject(path, key, "locator requires id, text, description, or type")
	}
	return loc, nil
}

func decodeString(path string, key, val *yaml.Node) (string, error) {
	if val.Kind != yaml.ScalarNode || val.Tag != "!!str" {
		return "", reject(path, val, fmt.Sprintf("field %q must be a string", key.Value))
	}
	return val.Value, nil
}

func noteCheck(path string, set *bool, key *yaml.Node) error {
	if *set {
		return reject(path, key, "assertion has more than one check")
	}
	*set = true
	return nil
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

func rejectMixed(path string, node *yaml.Node) error {
	var seenCases, seenCase bool
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		switch key.Value {
		case "cases":
			if seenCase {
				return reject(path, key, "file mixes case and suite fields")
			}
			seenCases = true
		case "name", "steps":
			if seenCases {
				return reject(path, key, "file mixes case and suite fields")
			}
			seenCase = true
		}
	}
	return reject(path, node, "file mixes case and suite fields")
}

func decodeSuite(path string, node *yaml.Node) (*Suite, error) {
	var cases *yaml.Node
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]
		if err := noteKey(path, seen, key); err != nil {
			return nil, err
		}
		switch key.Value {
		case "cases":
			cases = val
		default:
			return nil, reject(path, key, fmt.Sprintf("unknown field %q", key.Value))
		}
	}
	if cases == nil {
		return nil, reject(path, node, "missing cases")
	}
	if cases.Kind != yaml.SequenceNode {
		return nil, reject(path, cases, "cases must be a sequence")
	}
	if len(cases.Content) == 0 {
		return nil, reject(path, cases, "cases must not be empty")
	}
	suite := &Suite{Cases: make([]Case, 0, len(cases.Content))}
	for i, item := range cases.Content {
		loaded, err := loadReferencedCase(path, i+1, item)
		if err != nil {
			return nil, err
		}
		suite.Cases = append(suite.Cases, loaded)
	}
	return suite, nil
}

func loadReferencedCase(suitePath string, reference int, item *yaml.Node) (Case, error) {
	if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
		return Case{}, referenceRejection(suitePath, reference, item, "", &Rejection{
			Line:   item.Line,
			Column: item.Column,
			Reason: "case path must be a string",
		})
	}
	ref := item.Value
	if ref == "" {
		return Case{}, referenceRejection(suitePath, reference, item, "", &Rejection{
			Line:   item.Line,
			Column: item.Column,
			Reason: "case path must not be empty",
		})
	}
	if filepath.IsAbs(filepath.FromSlash(ref)) {
		return Case{}, referenceRejection(suitePath, reference, item, ref, &Rejection{
			Line:   item.Line,
			Column: item.Column,
			Reason: "case path must be relative",
		})
	}
	casePath := filepath.Clean(filepath.Join(filepath.Dir(suitePath), filepath.FromSlash(ref)))
	data, err := os.ReadFile(casePath)
	if err != nil {
		return Case{}, referenceRejection(suitePath, reference, item, casePath, &Rejection{
			Line:   item.Line,
			Column: item.Column,
			Reason: "cannot read file",
		})
	}
	node, err := parseFile(casePath, data)
	var loaded Case
	if err == nil {
		// A reference must be a case. Never expand a nested suite, even to validate it.
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Value == "cases" {
				err = reject(casePath, key, "referenced file is a suite")
				break
			}
		}
		if err == nil {
			loaded, err = decodeCase(casePath, node)
		}
	}
	if err != nil {
		inner, ok := err.(*Rejection)
		if !ok {
			return Case{}, referenceRejection(suitePath, reference, item, casePath, &Rejection{
				Line:   item.Line,
				Column: item.Column,
				Reason: err.Error(),
			})
		}
		return Case{}, referenceRejection(suitePath, reference, item, casePath, inner)
	}
	return loaded, nil
}

func referenceRejection(suitePath string, reference int, item *yaml.Node, casePath string, inner *Rejection) error {
	line, column := item.Line, item.Column
	var caseLine, caseColumn int
	// Only errors located inside the referenced file carry a case position.
	if inner.File != "" {
		caseLine, caseColumn = inner.Line, inner.Column
	}
	return &Rejection{
		File:       suitePath,
		Line:       line,
		Column:     column,
		Reason:     inner.Reason,
		Reference:  reference,
		CaseFile:   casePath,
		CaseLine:   caseLine,
		CaseColumn: caseColumn,
	}
}
