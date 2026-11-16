# Android 上保留 uiautomator2 服务

Android 的控件树、点击和输入依赖设备上的 uiautomator2 服务。执行时如果没有就安装，用例结束时不卸载。HarmonyOS NEXT 使用系统自带的 uitest，不安装额外应用。输入中文时 Android 上可能会切换输入法；用例结果确定之后再尝试切回，切不回只在报告里注明，不改变结果。

## Considered Options

- 只走 adb shell input：不用安装，但没有控件树，中文输入也弱。
- 每次执行后卸载：设备更干净，下次执行还要再装一遍。
- 装上并保留。
