# 02 在 Android 真机验证独立重跑与反例

Status: ready-for-human

Blocked by: [01](01-generate-cli-draft.md)

[所属规格](../spec.md)

以同一业务场景检验节省的编写时间和断言有效性。

## 验收条件

- [x] 记录应用起始界面、测试数据以及必要账号状态，手写和辅助编写采用同一目标。
- [x] 新用例在无模型配置时通过，已知反例在目标预期断言处失败，两次执行各有对应会话报告。
- [ ] 记录两种方式到首次独立通过的耗时，计入修动作、定位器和断言的时间。

## Comments

Xiaomi 14（Android 16）在 Firefox 打开本地测试商品列表，无需账号。起点为 Gnosis Amber 在第一行、Gnosis Blue 在第二行、通知关闭。两条编写路径都要打开 Amber 并断言详情。固定步骤在无模型配置下首次通过，交换行序后仍通过。让相同点击打开 Blue 详情后，第 2 步原断言失败。

Windows 工作台通过命名管道取真机界面，本地模型替身读取本次树及截图并返回坐标。最近一次自动验收到首次独立通过，手工编辑路径为 4.11 秒，辅助编辑路径为 4.14 秒，包含保存、复核操作和重跑。另一次验收中设备服务曾拒绝点击，随后两次复跑通过。人工阅读和决定如何修订所需时间仍待编写者实测。

复现时指定 GNOSIS_PILOT_SERIAL、GNOSIS_PILOT_ADB、GNOSIS_PILOT_CLI、GNOSIS_PILOT_DESKTOP，选填 GNOSIS_PILOT_OUTPUT 保留各次报告，再运行 `go test -tags mygo_nowebview ./cmd/gnosis-desktop -run TestAndroidAuthoringPilot -count=1 -v`。见[真机验收场景](../../../cmd/gnosis-desktop/android_pilot_windows_test.go)。
