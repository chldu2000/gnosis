# 02: 载入其余动作

**What to build:** 同一载入入口能读完第一版的全部动作，并在结果里补上默认时长。等待必须自带时长。比例越界仍能载入并保持原值。

**Blocked by:** 01: 载入一条带点击的用例

**Status:** resolved

在 01 的步骤种类上增加，每个步骤仍然恰好一个种类键：

- `launch`：必填 `app` 字符串，可选 `entry` 字符串
- `long_press`：必填 `locator`，可选 `duration`；缺省为 800 毫秒
- `tap_at`：必填数字 `x`、`y`，不检查是否在 0 到 1 之内
- `swipe`：必填数字 `from_x`、`from_y`、`to_x`、`to_y`，可选 `duration`；缺省为 300 毫秒；比例同样不限范围
- `type`：必填 `text` 字符串
- `key`：必填 `name`，只能是 `back` 或 `home`
- `wait`：必填 `duration`，没有默认值

时长是非负整数紧挨单位 `ms` 或 `s`，例如 `800ms`、`10s`。有空格、有小数、没有单位，都拒绝。

- [x] 每种动作都能载入，字段与文件一致
- [x] 长按和滑动没写时长时，结果里分别是 800 毫秒和 300 毫秒；写了则保留所写的值
- [x] 等待没写时长时拒绝
- [x] 比例小于 0 或大于 1 时仍载入，数值不变
- [x] 不认识的动作种类、按键名、时长写法都拒绝并指出位置
- [x] 载入不修改文件

## Comments

Implemented on `main` at `92f6124`. Covered by `load.go` and `load_actions_test.go`. The tracker has no separate closed triage role, so this uses `resolved`.
