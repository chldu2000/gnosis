# 从命令行执行用例或套件

构建 Windows 命令行程序，并按 [Android 执行说明](android-execution.md) 准备 ADB 服务和设备资源。用 `adb devices` 查看设备序列号。

```powershell
go build -o gnosis.exe ./cmd/gnosis
.\gnosis.exe --device SERIAL .\cases\smoke.yaml
.\gnosis.exe --device SERIAL .\cases\suite.yaml
```

参数放在用例或套件文件路径之前。套件中的文件全部载入后，程序才会连接指定设备。套件按引用顺序使用同一台设备，遇到失败或阻塞继续执行，遇到执行错误、设备离线或报告保存失败停止。

每次已开始的执行生成一份可离线打开的 HTML 报告。默认会话目录位于 Windows 的 `%LOCALAPPDATA%\gnosis\reports` 或 macOS 的 `~/Library/Caches/gnosis/reports` 下。传入 `--report-dir` 可为本次执行选择其他位置。相对路径从当前工作目录解析，指定的目录须位于用例、套件和套件引用的用例目录树之外。

```powershell
.\gnosis.exe --device SERIAL --report-dir '.\saved reports' .\cases\smoke.yaml
```

`report.html` 是报告入口，截图随报告保存到 `attachments/` 子目录。移动或分享报告时请复制整个会话文件夹。标准输出打印一次报告的绝对路径，诊断写到标准错误。所有用例均完成且结果为 `pass` 或 `executed` 时退出码为 `0`，其他结果以及参数、执行或保存错误返回 `1`。

执行 AI 步骤时，程序从宿主环境读取 `GNOSIS_MODEL_ENDPOINT`、`GNOSIS_MODEL_NAME` 和 `GNOSIS_MODEL_API_KEY`。模型地址为完整的 Chat Completions URL。可在文件路径前传入 `--model-endpoint` 或 `--model` 覆盖本次执行的地址或模型名。普通用例无需设置这些变量。

Go 调用方可对 `Run` 或 `RunSuite` 传入 `WithReports(directory)` 保存报告。从任意已保存的 `RunRecord.ReportPath` 取得本次执行的同一报告路径。载入时可传入 `ReportsOutsideCaseTree(directory)` 检查输出目录与输入目录是否分开。
