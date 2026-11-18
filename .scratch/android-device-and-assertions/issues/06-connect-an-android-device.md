# 06: 接到一台 Android 设备

**What to build:** 同一个执行入口可以接上一台选定的 Android 真机。控件树、手势、输入、启动应用和截图都通过这台设备完成；屏幕文字在本机识别。没有 uiautomator2 时装上并保留。前面各票用设备替身证明过的行为保持不变。

**Blocked by:** 02: 执行启动、按比例点击、滑动、输入和按键; 05: 用屏幕文字查找并判定

**Status:** ready-for-agent

真机用进程内的 adb 协议连接，不把 adb 可执行文件当作发布路径。控件树、手势、输入、启动应用和截图通过 uiautomator2。屏幕文字用可执行文件旁的 ONNX Runtime 与 PP-OCR 中文模型。缺动态库或模型时，识别为不可用，依赖屏幕文字的步骤因此是错误。

这台设备实现的操作只限于：报告是否在线、报告是否已被占用、占用、释放、确保 uiautomator2 已安装、读取屏幕宽高、读取控件树、截图、从截图识别屏幕文字及其区域、按像素点击、长按、滑动、向当前焦点输入文本、按返回或主页、按应用标识和可选入口启动应用、切回输入法。没有卸载，也没有任意命令。结束时不卸载 uiautomator2。

行为测试仍然只走执行入口和设备替身，不连接真机，也不检查协议字节或模型张量。本票不实现 HarmonyOS NEXT。

- [ ] 执行入口能接一台按设备标识选定的 Android 真机
- [x] 连接走进程内 adb 协议，不把 adb 可执行文件当作发布路径
- [ ] 真机提供同一组设备操作：在线与占用、安装并保留 uiautomator2、屏幕宽高、控件树、截图、像素上的点击、长按、滑动、输入、返回与主页、启动应用、输入法切回、屏幕文字及其区域
- [x] 结束时不卸载 uiautomator2，也不提供卸载或任意命令
- [x] 屏幕文字使用可执行文件旁的 ONNX Runtime 与 PP-OCR 中文模型；缺动态库或模型时识别为不可用
- [x] 已有行为测试仍只通过执行入口和设备替身，不连接真机

## Comments

已实现 Android 适配器，并通过本机模拟 ADB 与 uiautomator2 的执行检查以及真实中文 OCR 检查。待在已授权的 Android 真机上验证设备连接、安装、控件树、手势、输入法和截图。见 [Android 执行接入说明](../../../docs/android-execution.md)。

- 真机验证中发现两处问题，已在 `android.go` / `android_adb.go` 修复，代码检查与单元测试通过，真机尚未验证：
  1. adb shell 的后台进程可能被收掉。未改协议（仍走 v1 `shell:`），仅在启动命令前加条件式 `setsid`（设备没有时退回 `nohup`）。
  2. `/stop` 只关闭 HTTP 监听，uiautomator2 JVM 残留并占用 UiAutomation 注册，下一次执行失败。启动时回显 `$!` 得到的 PID；Release 先 `/stop`，再在 `/proc/<pid>/cmdline` 仍含 `com.wetest.uia2.Main` 时按 PID `kill -9`。
- 待真机确认：`setsid` 是否 fork 导致 `$!` 不是 JVM 的 PID（若是，KILL 会被守卫跳过，JVM 仍残留）；设备上是否有 `grep` 与 `command -v setsid`；同一设备连续执行两次，第二次能正常启动。
