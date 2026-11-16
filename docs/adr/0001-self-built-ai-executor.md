# AI 执行器自研，不封装 Mobilerun

Gnosis 要在 Android 和 HarmonyOS NEXT 上跑同一套 AI 步骤，发布物是 Go 程序。AI 执行器放在 Go 核心里：把界面交给 OpenAI 兼容接口的模型，模型只能返回已经声明的设备操作。不封装 Mobilerun，因为它不覆盖 HarmonyOS，而且会把 Python 运行时带进发布包。

## Considered Options

- 封装 Mobilerun：Android 上现成，但没有 HarmonyOS，发布包不再是单个 Go 程序。
- 自研执行器：两端同一条循环，动作和脚本共用一组。
