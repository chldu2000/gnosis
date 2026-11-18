# 在 Android 设备上执行用例

Windows 上先准备本机 ADB 服务，将测试设备通过 USB 接入并允许 USB 调试。用开发环境的 `adb devices` 确认序列号和 `device` 状态。Go 核心直接连到 `127.0.0.1:5037` 上的 ADB 服务，并按序列号选中设备。ADB 服务由开发环境提供。[Android SDK Platform-Tools](https://developer.android.com/tools/releases/platform-tools)

将下列文件放在调用 Go 核心的可执行文件旁。缺少 OCR 文件只会影响需要识别屏幕文字的步骤。

| 文件 | 来源与用途 |
| --- | --- |
| `u2.jar` | [uiautomator2 JAR 0.4.0](https://github.com/openatx/android-uiautomator-server-jar/releases/tag/0.4.0)。运行时在设备上首次安装，供控件树、手势和截图使用。 |
| `app-uiautomator.apk` | [AdbKeyboard 2.4.0](https://github.com/openatx/android-uiautomator-server/releases/tag/2.4.0)。首次替换文本时按需安装，执行结束后恢复原输入法。 |
| `onnxruntime.dll` | [ONNX Runtime 1.23.2](https://www.nuget.org/packages/Microsoft.ML.OnnxRuntime/1.23.2)。取与程序架构一致的原生库，例如 NuGet 包中的 `runtimes/win-x64/native/onnxruntime.dll`。 |
| `ch_PP-OCRv5_mobile_det.onnx` | [PP-OCRv5 中文移动检测模型](https://www.modelscope.cn/models/RapidAI/RapidOCR/resolve/v3.4.0/onnx/PP-OCRv5/det/ch_PP-OCRv5_mobile_det.onnx)。 |
| `ch_PP-OCRv5_rec_mobile_infer.onnx` | [PP-OCRv5 中文移动识别模型](https://www.modelscope.cn/models/RapidAI/RapidOCR/resolve/v3.4.0/onnx/PP-OCRv5/rec/ch_PP-OCRv5_rec_mobile_infer.onnx)。 |
| `ppocrv5_dict.txt` | [与模型配套的 18,383 项中文字典](https://huggingface.co/monkt/paddleocr-onnx/resolve/7b02d0a30a07ba2b92ad1ff5a8941ae2c633de65/languages/chinese/dict.txt)。将下载的 `dict.txt` 改为此文件名。 |

也可从 [uiautomator2 3.7.1 的 wheel](https://pypi.org/project/uiautomator2/3.7.1/#files) 解压 `uiautomator2/assets/` 内的两个设备文件。该发行包包含 JAR 0.4.1 和输入法 2.4.0。

在 Go 调用方先载入用例，再运行。用例内容可以先写成 `name: Android smoke` 加一个 `wait` 步骤，确认设备安装、执行记录和截图。

```go
func executeAndroid(path, serial string) (*gnosis.RunRecord, error) {
    testCase, suite, err := gnosis.Load(path)
    if err != nil {
        return nil, err
    }
    if suite != nil {
        return nil, errors.New("expected a case file")
    }
    device, err := gnosis.NewAndroidDevice(serial)
    if err != nil {
        return nil, err
    }
    return gnosis.Run(testCase, device, gnosis.RealClock{})
}
```

`Run` 拒绝开始时返回错误和空记录。开始后返回执行记录，结果在 `record.Result`，操作错误在 `record.Error`，每步的结束图像在 `record.Steps[i].Screenshot`。同一 Windows 用户下，各 Gnosis 进程以设备序列号共享占用锁。设备端 JAR 和输入法会留在设备上。Gnosis 结束时停止自己开启的 uiautomator2 监听。
