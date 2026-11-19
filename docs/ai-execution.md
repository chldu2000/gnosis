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
