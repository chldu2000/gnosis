package gnosis

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
)

// InterfaceCapture groups screen pixels and tree coordinates from one observation.
type InterfaceCapture struct {
	Size       image.Point
	Elements   []Element
	Screenshot []byte `json:",omitempty"`
}

func captureFromObservation(tree []Element, pixels []byte) *InterfaceCapture {
	config, _, err := image.DecodeConfig(bytes.NewReader(pixels))
	if err != nil {
		return nil
	}
	return &InterfaceCapture{Size: image.Pt(config.Width, config.Height), Elements: tree, Screenshot: pixels}
}

// InspectDevice takes a read-only observation under the ordinary device reservation.
func InspectDevice(ctx context.Context, device Device) (*InterfaceCapture, error) {
	release, err := occupyRunDevice(device)
	if err != nil {
		return nil, err
	}
	defer cleanupDevice(device, func() error { release(); return nil })
	setDeviceContext(device, ctx)
	if err := device.EnsureAutomation(); err != nil {
		return nil, err
	}
	return observeInterface(device)
}

func observeInterface(device Device) (*InterfaceCapture, error) {
	tree, err := device.ControlTree()
	if err != nil {
		return nil, err
	}
	pixels, err := device.Screenshot()
	if err != nil {
		return nil, err
	}
	capture := captureFromObservation(tree, pixels)
	if capture == nil {
		return nil, errors.New("invalid device screenshot")
	}
	return capture, nil
}

func matchTree(l Locator, tree []Element) []Element {
	var matches []Element
	for _, e := range tree {
		if matchesField(l.ID, e.ID) && matchesField(l.Text, e.Text) && matchesField(l.Description, e.Description) && matchesField(l.Type, e.Type) {
			matches = append(matches, e)
		}
	}
	return matches
}

type LocatorCandidate struct {
	Locator Locator
	Count   int
}

// Candidates uses the same field comparison and traversal order as execution.
func (c *InterfaceCapture) Candidates(index int) []LocatorCandidate {
	if index < 0 || index >= len(c.Elements) {
		return nil
	}
	e := c.Elements[index]
	var result []LocatorCandidate
	add := func(l Locator) { result = append(result, LocatorCandidate{l, len(matchTree(l, c.Elements))}) }
	if e.ID != "" {
		add(Locator{ID: &e.ID})
	}
	if e.Description != "" {
		add(Locator{Description: &e.Description})
	}
	if e.Text != "" {
		add(Locator{Text: &e.Text})
	}
	if e.ID != "" && e.Text != "" {
		add(Locator{ID: &e.ID, Text: &e.Text})
	}
	if e.Type != "" {
		if e.Text != "" && len(matchTree(Locator{Text: &e.Text}, c.Elements)) > 1 {
			add(Locator{Type: &e.Type, Text: &e.Text})
		}
		if e.Description != "" && len(matchTree(Locator{Description: &e.Description}, c.Elements)) > 1 {
			add(Locator{Type: &e.Type, Description: &e.Description})
		}
		add(Locator{Type: &e.Type})
	}
	return result
}

// Hit selects the deepest visible element. The tree also allows selecting ancestors.
func (c *InterfaceCapture) Hit(p image.Point) int {
	selected := -1
	for i, e := range c.Elements {
		if p.In(e.Bounds) && (selected < 0 || e.Depth >= c.Elements[selected].Depth) {
			selected = i
		}
	}
	return selected
}
