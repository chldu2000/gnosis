# 03: 载入断言和 AI 步骤

**What to build:** 同一条用例里可以混写断言和 AI 步骤。每条断言必须标明前置或预期。载入结果里带上默认时限和默认步数上限。

**Blocked by:** 02: 载入其余动作

**Status:** resolved

步骤的种类键再增加三种，仍然恰好一个：

- `precondition` 或 `expect`：断言体。断言体里恰好一个检查键，外加可选 `timeout`。检查键是 `exists`、`absent`（值为定位器）、`text_equals`、`text_contains`（定位器加必填字符串 `text`）、`attr_equals`（定位器，加 `attribute` 只能是 `enabled` 或 `checked`，加布尔 `value`）、`screen_text`（必填字符串 `text`，没有定位器）。没写 `timeout` 时结果为 10 秒。
- `ai`：必填字符串 `goal`，可选正整数 `max_steps`。没写时结果为 20。

不写 `precondition` 或 `expect` 就不能载入断言。一个步骤里同时出现两种位置，或两种检查，都拒绝。

- [x] 六种检查、两种位置都能载入，并和动作、AI 步骤按文件顺序排在同一条用例里
- [x] 没写时限时结果是 10 秒；没写步数上限时结果是 20；写了则保留
- [x] 只有 AI 步骤的用例可以载入
- [x] 断言没写位置、检查种类不认识、属性名不认识、步数上限不是正整数，都拒绝并指出位置
- [x] 载入不修改文件

## Comments

Implemented on `main` at `92f6124`. Covered by `load.go` and `load_assertions_test.go`. The tracker has no separate closed triage role, so this uses `resolved`.
