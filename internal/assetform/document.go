// Package assetform edits the original YAML syntax tree so that form edits
// preserve comments and fields the file author leaves implicit.
package assetform

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"gnosis"
	"gnosis/internal/fileio"
)

var ErrChangedOnDisk = errors.New("the file changed on disk after it was opened")

type Document struct {
	path      string
	root      *yaml.Node
	original  []byte
	digest    [sha256.Size]byte
	dirty     bool
	suite     bool
	draftRows map[*yaml.Node]int
}

func FromDraft(draft *gnosis.CaseDraft) (*Document, error) {
	if err := gnosis.ValidateAsset("draft.yaml", []byte(draft.YAML)); err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(draft.YAML), &root); err != nil {
		return nil, err
	}
	d := &Document{root: &root, dirty: true, draftRows: make(map[*yaml.Node]int)}
	for i, row := range d.rows() {
		d.draftRows[row] = i
	}
	return d, nil
}

// DraftRow keeps review evidence associated with its original step after edits.
func (d *Document) DraftRow(i int) (int, bool) {
	if i < 0 || i >= d.Rows() {
		return 0, false
	}
	v, ok := d.draftRows[d.rows()[i]]
	return v, ok
}

// ApplyLocator preserves other parameters (including the author's expected value).
func (d *Document) ApplyLocator(i int, l gnosis.Locator) error {
	if i < 0 || i >= d.Rows() || d.Suite() {
		return errors.New("请先选择一个用例步骤")
	}
	kind := d.StepKind(i)
	row := d.rows()[i]
	var node *yaml.Node
	key := "locator"
	switch kind {
	case "tap", "long_press":
		node = ensureMapping(row, kind)
	case "expect", "precondition":
		check := d.CheckKind(i)
		node = child(row, kind)
		switch check {
		case "exists", "absent":
			key = check
		case "attr_equals", "text_equals", "text_contains":
			node = ensureMapping(node, check)
		default:
			return errors.New("此断言没有定位器")
		}
	default:
		return errors.New("此步骤没有定位器")
	}
	target := mapping()
	for _, item := range []struct {
		k string
		v *string
	}{{"id", l.ID}, {"text", l.Text}, {"description", l.Description}, {"type", l.Type}} {
		if item.v != nil {
			setScalar(target, item.k, "!!str", *item.v)
		}
	}
	if l.Index > 0 {
		setScalar(target, "index", "!!int", strconv.Itoa(l.Index))
	}
	set(node, key, target)
	d.dirty = true
	return nil
}

func Open(path string) (*Document, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, gnosis.ValidateAsset(path, data)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var root, extra yaml.Node
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%s: expected one YAML document", path)
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: expected a case or suite mapping", path)
	}
	doc := &Document{path: path, root: &root, original: data, digest: sha256.Sum256(data)}
	doc.suite = child(doc.mapping(), "cases") != nil
	return doc, nil
}

func New(suite bool) *Document {
	doc := &Document{root: &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping()}}, suite: suite, dirty: true}
	if suite {
		doc.ensureRows()
	}
	return doc
}

func (d *Document) Path() string        { return d.path }
func (d *Document) Dirty() bool         { return d.dirty }
func (d *Document) Suite() bool         { return d.suite }
func (d *Document) mapping() *yaml.Node { return d.root.Content[0] }

func (d *Document) RefersTo(path string) bool { return samePath(d.path, path) }

// MatchesSavedVersion reports whether the editor still represents the loaded file version.
func (d *Document) MatchesSavedVersion(digest [sha256.Size]byte) bool {
	return d.path != "" && !d.dirty && d.digest == digest
}

func (d *Document) Name() string  { return value(d.mapping(), "name") }
func (d *Document) HasName() bool { return child(d.mapping(), "name") != nil }

func (d *Document) SetName(name string) {
	d.dirty = setScalar(d.mapping(), "name", "!!str", name) || d.dirty
}

func (d *Document) ClearName() { d.dirty = remove(d.mapping(), "name") || d.dirty }

func (d *Document) rowKey() string {
	if d.suite {
		return "cases"
	}
	return "steps"
}

func (d *Document) rows() []*yaml.Node {
	list := child(d.mapping(), d.rowKey())
	if list == nil || list.Kind != yaml.SequenceNode {
		return nil
	}
	return list.Content
}

func (d *Document) ensureRows() *yaml.Node {
	key := d.rowKey()
	list := child(d.mapping(), key)
	if list == nil || list.Kind != yaml.SequenceNode {
		list = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		set(d.mapping(), key, list)
	}
	return list
}

func (d *Document) Rows() int { return len(d.rows()) }

func (d *Document) AddStep() {
	row := mapping()
	set(row, "wait", mapping())
	setScalar(child(row, "wait"), "duration", "!!str", "1s")
	d.addRow(row)
}

func (d *Document) AddReference(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if d.path != "" {
		path, err = filepath.Rel(filepath.Dir(d.path), path)
		if err != nil {
			return fmt.Errorf("cannot reference that case from this suite: %w", err)
		}
	}
	node := &yaml.Node{}
	node.SetString(filepath.ToSlash(path))
	d.addRow(node)
	return nil
}

func (d *Document) addRow(row *yaml.Node) {
	list := d.ensureRows()
	list.Content = append(list.Content, row)
	d.dirty = true
}

func (d *Document) RemoveRow(i int) {
	list := d.ensureRows()
	list.Content = slices.Delete(list.Content, i, i+1)
	d.dirty = true
}

func (d *Document) MoveRow(i, by int) {
	rows := d.rows()
	dst := i + by
	if dst >= 0 && dst < len(rows) {
		rows[i], rows[dst] = rows[dst], rows[i]
		d.dirty = true
	}
}

func (d *Document) Reference(i int) string { return d.rows()[i].Value }

func (d *Document) StepKind(i int) string {
	row := d.rows()[i]
	for _, kind := range StepKinds {
		if child(row, kind) != nil {
			return kind
		}
	}
	return ""
}

func (d *Document) SetStepKind(i int, kind string) {
	if d.StepKind(i) == kind {
		return
	}
	row := d.rows()[i]
	if row.Kind != yaml.MappingNode {
		*row = *mapping()
	}
	for _, prior := range StepKinds {
		remove(row, prior)
	}
	set(row, kind, mapping())
	d.dirty = true
}

func (d *Document) CheckKind(i int) string {
	assertion := child(d.rows()[i], d.StepKind(i))
	for _, kind := range CheckKinds {
		if child(assertion, kind) != nil {
			return kind
		}
	}
	return ""
}

func (d *Document) SetCheckKind(i int, kind string) {
	current := d.CheckKind(i)
	if current == kind {
		return
	}
	assertion := ensureMapping(d.rows()[i], d.StepKind(i))
	locator := child(assertion, current)
	if current != "exists" && current != "absent" {
		locator = child(locator, "locator")
	}
	for _, prior := range CheckKinds {
		remove(assertion, prior)
	}
	check := mapping()
	if locator != nil && kind != "screen_text" {
		if kind == "exists" || kind == "absent" {
			check = locator
		} else {
			set(check, "locator", locator)
		}
	}
	set(assertion, kind, check)
	d.dirty = true
}

func (d *Document) Field(i int, field Field) (string, bool) {
	node := d.rows()[i]
	for _, key := range field.path {
		node = child(node, key)
	}
	if node == nil {
		return "", false
	}
	return node.Value, true
}

func (d *Document) SetField(i int, field Field, text string) {
	row := d.rows()[i]
	for _, key := range field.path[:len(field.path)-1] {
		row = ensureMapping(row, key)
	}
	tag := field.tag
	if tag == "number" {
		tag = "!!float"
		if _, err := strconv.Atoi(text); err == nil {
			tag = "!!int"
		}
	}
	d.dirty = setScalar(row, field.path[len(field.path)-1], tag, text) || d.dirty
}

func (d *Document) ClearField(i int, field Field) {
	row := d.rows()[i]
	for _, key := range field.path[:len(field.path)-1] {
		row = child(row, key)
	}
	d.dirty = remove(row, field.path[len(field.path)-1]) || d.dirty
}

func (d *Document) Validate() error {
	if !d.dirty && d.path != "" {
		return gnosis.ValidateAsset(d.path, d.original)
	}
	path := d.path
	if path == "" {
		path = "draft.yaml"
	}
	data, err := encode(d.root)
	if err != nil {
		return err
	}
	return gnosis.ValidateAsset(path, data)
}

func (d *Document) Save(destination string) error {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	current := samePath(d.path, destination)
	if current && !d.dirty {
		if err := d.checkOriginal(); err != nil {
			return err
		}
		return d.Validate()
	}
	root := d.root
	if d.suite {
		root = clone(d.root)
		if err := d.rebase(root, destination); err != nil {
			return err
		}
	}
	data, err := encode(root)
	if err != nil {
		return err
	}
	if err := gnosis.ValidateAsset(destination, data); err != nil {
		return err
	}
	if current {
		if err := d.checkOriginal(); err != nil {
			return err
		}
	}
	write := fileio.Replace
	if d.draftRows != nil && d.path == "" {
		write = fileio.Create
	}
	if err := write(destination, data); err != nil {
		return err
	}
	d.root, d.path, d.original, d.dirty = root, destination, data, false
	d.digest = sha256.Sum256(data)
	return nil
}

func (d *Document) checkOriginal() error {
	data, err := os.ReadFile(d.path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !bytes.Equal(data, d.original)) {
		return ErrChangedOnDisk
	}
	return err
}

func (d *Document) rebase(root *yaml.Node, destination string) error {
	list := child(root.Content[0], "cases")
	if list == nil || list.Kind != yaml.SequenceNode {
		return nil
	}
	for i, item := range list.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" || item.Value == "" {
			continue // The shared loader reports the invalid reference.
		}
		target := filepath.FromSlash(item.Value)
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(d.path), target)
		}
		target, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if samePath(target, destination) {
			return fmt.Errorf("reference %d (%s) is the destination file", i+1, item.Value)
		}
		if samePath(d.path, destination) && !filepath.IsAbs(filepath.FromSlash(item.Value)) {
			continue
		}
		relative, err := filepath.Rel(filepath.Dir(destination), target)
		if err != nil {
			return fmt.Errorf("reference %d (%s) cannot be relative to the destination: %w", i+1, item.Value, err)
		}
		item.Value = filepath.ToSlash(relative)
	}
	return nil
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b || (runtime.GOOS == "windows" && strings.EqualFold(a, b)) {
		return true
	}
	left, leftErr := os.Stat(a)
	right, rightErr := os.Stat(b)
	return leftErr == nil && rightErr == nil && os.SameFile(left, right)
}

func encode(root *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	return buf.Bytes(), encoder.Close()
}

func clone(node *yaml.Node) *yaml.Node {
	result := *node
	result.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		result.Content[i] = clone(child)
	}
	return &result
}

func mapping() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }

func child(node *yaml.Node, key string) *yaml.Node {
	if node != nil && node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				return node.Content[i+1]
			}
		}
	}
	return nil
}

func value(node *yaml.Node, key string) string {
	if val := child(node, key); val != nil {
		return val.Value
	}
	return ""
}

func set(node *yaml.Node, key string, val *yaml.Node) {
	if previous := child(node, key); previous != nil {
		val.HeadComment, val.LineComment, val.FootComment = previous.HeadComment, previous.LineComment, previous.FootComment
		*previous = *val
		return
	}
	keyNode := &yaml.Node{}
	keyNode.SetString(key)
	node.Content = append(node.Content, keyNode, val)
}

func ensureMapping(node *yaml.Node, key string) *yaml.Node {
	val := child(node, key)
	if val == nil || val.Kind != yaml.MappingNode {
		set(node, key, mapping())
		val = child(node, key)
	}
	return val
}

func setScalar(node *yaml.Node, key, tag, text string) bool {
	previous := child(node, key)
	if previous != nil && previous.Kind == yaml.ScalarNode && previous.Value == text && previous.Tag == tag {
		return false
	}
	if node.Kind != yaml.MappingNode {
		*node = *mapping()
	}
	set(node, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: text})
	return true
}

func remove(node *yaml.Node, key string) bool {
	if node != nil && node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				node.Content = slices.Delete(node.Content, i, i+2)
				return true
			}
		}
	}
	return false
}
