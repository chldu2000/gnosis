# 屏幕文字用 ONNX Runtime 加载 PP-OCR，放在核心里

定位器的后备查找和「屏幕上看得见这段文字」断言，都用 ONNX Runtime 加载 PP-OCR 中文模型。动态库和模型放在可执行文件旁边，不带 Python。不用纯 Go 的 OCR，因为中文识别不够用；不把识别交给在线视觉接口，因为执行要能离线，而且截图不应离开本机。

## Considered Options

- 纯 Go OCR：没有足够好的中文模型，形近字多。
- 在线视觉接口：需要网络，截图会发给外部服务，执行结果也依赖它的可用性。
- ONNX Runtime 加 PP-OCR：离线，中文效果可用，代价是要随发布物分发动态库和模型。

## Consequences

- 发布物除了 Go 程序，还要带 ONNX Runtime 动态库和 PP-OCR 模型文件。
- OCR 引擎不可用时，屏幕文字断言和定位器后备都会得到错误，而不是失败。
