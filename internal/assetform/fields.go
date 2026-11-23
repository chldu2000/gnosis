package assetform

import (
	"slices"
	"strings"
)

type Field struct {
	Label    string
	Choices  []string
	Optional bool
	path     []string
	tag      string
}

var StepKinds = []string{"launch", "tap", "long_press", "tap_at", "swipe", "type", "key", "wait", "precondition", "expect", "ai"}
var CheckKinds = []string{"exists", "absent", "text_equals", "text_contains", "attr_equals", "screen_text"}

func field(path, label, tag string, optional bool, choices ...string) Field {
	return Field{Label: label, Choices: choices, Optional: optional, path: strings.Split(path, "."), tag: tag}
}

var StepName = field("name", "步骤名称", "!!str", true)

var actionFields = map[string][]Field{
	"launch": {
		field("app", "应用标识", "!!str", false), field("entry", "入口", "!!str", true),
	},
	"long_press": {field("duration", "长按时长", "!!str", true)},
	"tap_at":     {field("x", "横坐标比例", "number", false), field("y", "纵坐标比例", "number", false)},
	"swipe": {
		field("from_x", "起点横坐标", "number", false), field("from_y", "起点纵坐标", "number", false),
		field("to_x", "终点横坐标", "number", false), field("to_y", "终点纵坐标", "number", false),
		field("duration", "滑动时长", "!!str", true),
	},
	"type": {field("text", "替换文本", "!!str", false)},
	"key":  {field("name", "按键", "!!str", false, "back", "home")},
	"wait": {field("duration", "等待时长", "!!str", false)},
	"ai":   {field("goal", "目标", "!!str", false), field("max_steps", "步数上限", "!!int", true)},
}

var locatorFields = []Field{
	field("id", "元素 ID", "!!str", true), field("text", "显示文本", "!!str", true),
	field("description", "描述", "!!str", true), field("type", "控件类型", "!!str", true),
	field("index", "匹配序号", "!!int", true),
	field("relative.relation", "参照关系", "!!str", true, "within", "right_of", "left_of", "above", "below"),
	field("relative.levels", "从参照向上的层数", "!!int", true),
	field("relative.anchor.id", "参照 ID", "!!str", true), field("relative.anchor.text", "参照文本", "!!str", true),
	field("relative.anchor.description", "参照描述", "!!str", true), field("relative.anchor.type", "参照类型", "!!str", true),
}

func Fields(kind, check string) []Field {
	fields := []Field{StepName}
	if kind != "precondition" && kind != "expect" {
		if kind == "tap" || kind == "long_press" {
			fields = append(fields, under(locatorFields, kind, "locator")...)
		}
		return append(fields, under(actionFields[kind], kind)...)
	}
	fields = append(fields, field(kind+".timeout", "断言时限", "!!str", true))
	switch check {
	case "exists", "absent":
		fields = append(fields, under(locatorFields, kind, check)...)
	case "text_equals", "text_contains":
		fields = append(fields, under(locatorFields, kind, check, "locator")...)
		fields = append(fields, field(kind+"."+check+".text", "期望文本", "!!str", false))
	case "attr_equals":
		fields = append(fields, under(locatorFields, kind, check, "locator")...)
		fields = append(fields, field(kind+".attr_equals.attribute", "属性", "!!str", false, "enabled", "checked"))
		fields = append(fields, field(kind+".attr_equals.value", "期望值", "!!bool", false, "true", "false"))
	case "screen_text":
		fields = append(fields, field(kind+".screen_text.text", "屏幕文字", "!!str", false))
	}
	return fields
}

func under(fields []Field, prefix ...string) []Field {
	result := make([]Field, len(fields))
	for i, field := range fields {
		field.path = append(slices.Clone(prefix), field.path...)
		result[i] = field
	}
	return result
}
