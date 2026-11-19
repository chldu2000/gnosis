# 在 Windows 桌面端编辑和执行

构建 MyGo 原生桌面程序，并按 [Android 执行说明](android-execution.md) 启动 ADB 服务。将对应的安卓资源放在生成的程序旁边。

```powershell
go build -tags mygo_nowebview -ldflags '-H windowsgui' -o Gnosis.exe ./cmd/gnosis-desktop
.\Gnosis.exe
```

在「用例与套件」打开现有 YAML 或新建文件。点击左侧列表中的步骤后，可以在右侧选择步骤类型和填写字段。套件通过文件选择器添加引用，并可调整顺序或重复引用同一个用例。开始执行前，有未保存的编辑时会提示保存和校验。

在「执行」刷新设备列表，明确选定一台在线的 Android 设备，然后点击「开始执行」。暂停在安全点生效，已暂停的执行会继续占用设备。关闭窗口后，可重新打开从原位置查看状态和继续控制。若显示「启动待确认」，可以先点击「核对启动」来查找上次请求。

单用例结束并保存报告后，桌面端会打开系统浏览器。套件执行页面按用例顺序显示各自的报告链接。HTML 位于 `%LOCALAPPDATA%\gnosis\reports`，可以离线打开。

需要执行 AI 步骤时，在「模型设置」填写完整的 Chat Completions URL、模型名称和 API 密钥。模型地址和名称保存在当前 Windows 用户的宿主设置中，密钥保存在 Windows 凭据管理器。修改从下一次执行生效。

连续暂停的默认时限为 30 分钟。可在启动桌面端前用 `GNOSIS_PAUSE_LIMIT` 设置独立执行进程的暂停时限，例如 `10m`。命令行用法见 [CLI 执行说明](cli-execution.md)。
