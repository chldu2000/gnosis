# 用协议库连接设备，不调用 adb 与 hdc 可执行文件

核心用 Go 实现的 adb 协议和 hdc 协议连接设备。Android 走 uiautomator2 服务，HarmonyOS NEXT 走 uitest。调用系统里的 adb、hdc 可执行文件只留作调试时的后备，不进发布路径。这样发布物不必带上这两个命令行工具，也不受它们的路径和输出格式变化影响。

## Considered Options

- 子进程调用 adb、hdc：实现快，但要把可执行文件打进包，输出解析也脆弱。
- 协议库：连接和文件传输走协议，各平台能做哪些操作仍分别声明。
