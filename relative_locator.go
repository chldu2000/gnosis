package gnosis

import "image"

// RelativeMatches describes indices in one preorder tree, before any target index is applied.
type RelativeMatches struct {
	Anchors, Targets []int
	Container        int // -1 for a spatial relation or unresolved scope
}

func parentNode(tree []Element, i int) int {
	for j := i - 1; j >= 0; j-- {
		if tree[j].Depth < tree[i].Depth {
			if tree[j].Depth == tree[i].Depth-1 {
				return j
			}
			break
		}
	}
	return -1
}

func inDirection(relation string, target, anchor image.Rectangle) bool {
	if target.Empty() || anchor.Empty() {
		return false
	}
	horizontal := target.Min.X < anchor.Max.X && anchor.Min.X < target.Max.X
	vertical := target.Min.Y < anchor.Max.Y && anchor.Min.Y < target.Max.Y
	switch relation {
	case "right_of":
		return vertical && target.Min.X >= anchor.Max.X
	case "left_of":
		return vertical && target.Max.X <= anchor.Min.X
	case "above":
		return horizontal && target.Max.Y <= anchor.Min.Y
	case "below":
		return horizontal && target.Min.Y >= anchor.Max.Y
	}
	return false
}

func (c *InterfaceCapture) PreviewRelative(l Locator) RelativeMatches {
	tree := c.Elements
	result := RelativeMatches{Container: -1}
	r := l.Relative
	if r == nil {
		return result
	}
	for i, e := range tree {
		if matchesElement(r.Anchor, e) {
			result.Anchors = append(result.Anchors, i)
		}
	}
	if len(result.Anchors) != 1 {
		return result
	}
	anchor := result.Anchors[0]
	first, end := 0, len(tree)
	if r.Relation == "within" {
		first = anchor
		for n := 0; n < r.Levels; n++ {
			first = parentNode(tree, first)
			if first < 0 {
				return result
			}
		}
		result.Container = first
		end = first + 1
		for end < len(tree) && tree[end].Depth > tree[first].Depth {
			end++
		}
	}
	for i := first; i < end; i++ {
		if i == anchor || !matchesElement(l, tree[i]) {
			continue
		}
		if r.Relation == "within" || inDirection(r.Relation, tree[i].Bounds, tree[anchor].Bounds) {
			result.Targets = append(result.Targets, i)
		}
	}
	return result
}

// RelationsBetween suggests only rules connecting the two selected nodes in this capture.
func (c *InterfaceCapture) RelationsBetween(target, anchor int, anchorLocator Locator) []RelativeLocator {
	tree := c.Elements
	if target < 0 || target >= len(tree) || anchor < 0 || anchor >= len(tree) || target == anchor {
		return nil
	}
	var rules []RelativeLocator
	t, a := target, anchor
	levels := 0
	for t != a && t >= 0 && a >= 0 {
		if tree[t].Depth > tree[a].Depth {
			t = parentNode(tree, t)
		} else {
			a = parentNode(tree, a)
			levels++
		}
	}
	if a >= 0 && a == t {
		rules = append(rules, RelativeLocator{Relation: "within", Levels: levels, Anchor: anchorLocator})
	}
	for _, relation := range []string{"right_of", "left_of", "above", "below"} {
		if inDirection(relation, tree[target].Bounds, tree[anchor].Bounds) {
			rules = append(rules, RelativeLocator{Relation: relation, Anchor: anchorLocator})
		}
	}
	return rules
}
