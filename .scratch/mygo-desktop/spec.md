Status: resolved

# Windows 桌面表单与本机执行控制

通过 Windows 原生桌面表单编辑用例和套件，然后选定一台设备执行并查看报告。桌面端使用共享 Go 核心校验文件，与独立的本机执行进程通过授权的 Windows 命名管道通信。执行进程管理暂停、恢复、取消和进度。重开窗口后恢复原执行或最近一次记录。

- 在暂停、取消或断线时，按[执行控制约定](../../docs/execution-control.md)保留设备占用、已确定结果和报告。
- 原生表单按[首版设计](../../docs/design.md)保留 YAML 注释、未填写的默认字段及套件引用。CLI 沿用现有用法。
- 宿主凭据保存在 Windows 凭据管理器。每次执行使用启动时的模型设置。

## 工单

1. [共享核心的执行控制](issues/01-control-shared-execution.md)
2. [独立的本机执行进程](issues/02-serve-local-execution.md)
3. [原生用例与套件表单](issues/03-edit-case-and-suite-forms.md)
4. [桌面控制与真机验收](issues/04-connect-the-desktop-to-execution.md)

## 验收

- Windows 下 `go test -count=1 ./...`、`go vet ./...` 和 `go build -tags mygo_nowebview ./...` 通过。凭据读写在登录会话中使用独立测试项验证并清理。
- 在最终 Windows 桌面构建中，选定安卓真机，执行包含重复引用的三条用例。在第二条用例暂停并关闭窗口，CLI 请求同一台设备被拒绝。重开窗口恢复同一个执行，继续后取消。第一条的完整报告、第二条的部分报告均包含真机截图，第三条保持未开始。
- 在原生表单修改用例并保存，保留原 YAML 注释。用桌面端执行该用例后，系统浏览器自动打开报告。CLI 随后成功在同一设备上执行同一份文件。另用原生表单保存 AI 步骤和断言，确认省略的默认设置保持省略。

见[桌面使用说明](../../docs/desktop-execution.md)。
