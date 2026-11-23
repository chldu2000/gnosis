# 在用例中执行 AI 步骤

调用方从执行环境读取模型设置，通过 `WithModel` 传给现有的 `Run`。用例继续用 `goal` 和 `max_steps` 描述 AI 步骤。省略上限时，加载器设为 20。

```go
record, err := gnosis.Run(testCase, device, gnosis.RealClock{}, gnosis.WithModel(gnosis.ModelConfig{
    Endpoint: modelEndpoint,
    Name:     modelName,
    APIKey:   modelKey,
}))
```

`Endpoint` 是支持图像的 OpenAI 兼容 Chat Completions 完整地址，一般以 `/v1/chat/completions` 结尾。每轮以非流式请求发送该步目标、当前控件树和截图。远程服务应使用 HTTPS，调用方应只选用允许接收被测界面内容的服务。可通过 `Timeout` 设置每轮请求时限，零值表示 30 秒。[Chat Completions 接口](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)

模型每轮可以选择停手或请求一个现有动作。动作短说明最多 512 个 Unicode 字符，原文保存在 `record.Steps[i].Trajectory`。轨迹同时保存动作参数和结束截图。AI 步骤停手或用完上限时结果为 `executed`，用例是否通过由预期断言决定。模型、观察或动作出错时，`record.Error` 返回诊断，已记录的轨迹可继续读取。

## 编写目标与预期断言

把允许模型自行选择路径的部分写成 `ai` 目标，把需要验证的行为写成独立的 `expect` 步骤。例如，要从设置首页找到通知偏好，可以交给 AI 导航，再用 `attr_equals` 检查开关的 `checked` 属性。如果要验证搜索按钮本身，就把「输入关键词并点击搜索按钮」写成固定动作，再检查指定商品是否出现在结果列表里。模型也可能通过点击搜索历史找到同一商品，那条轨迹覆盖的是另一个入口。Slack 在 Web 实验中也观察到智能体通过不同路径达成相同目标。 [Slack 的端到端测试实验](https://slack.engineering/agentic-testing-where-agents-fit-in-the-e2e-testing-stack/)

工作台后续可用「AI 执行」标识会操作设备的步骤，沿用现有 `ai` 格式。模型请求的点击、输入等动作继续复用普通步骤的动作集合。 [步骤设计](design.md#steps)

## 视觉语义断言的后续评估

建议先完成轨迹生成草稿和无模型重跑的验收。随后收集现有控件属性、显示文本和屏幕文字断言难以表达的真实场景。例如，放大系统字体后，控件树仍包含完整的「保存」二字，用户看到的按钮文字却可能被裁切。可收集已知正常与已知裁切的截图，多次判定并记录误判和漏判，再决定是否在 `expect` 下增加视觉语义断言。 [用例编写路线图](roadmap.md#after-the-first-release--ai-assisted-case-authoring)

候选方案中，模型只观察断言所在步骤的当前界面。编写者需要滚动或关闭弹窗时，应将这些动作显式写在断言前。若界面明确违反预期，结果为 `fail`。若截图读取或模型调用失败，结果为 `error`。报告应保留判定条件、对应截图及简短依据。含这类断言的用例在重跑时仍依赖模型。
