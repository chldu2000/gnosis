# 在 Android 设备上执行用例

用 Android SDK Platform-Tools 运行 `adb start-server`，将测试设备通过 USB 接入并允许 USB 调试。用 `adb devices -l` 确认序列号和 `device` 状态。Go 核心直接连到 `127.0.0.1:5037` 上的 ADB 服务，并按序列号选中设备。 [Android SDK Platform-Tools](https://developer.android.com/tools/releases/platform-tools)

如果 Gnosis 提示设备离线，先运行 `adb devices -l` 查看原因。

- ADB 服务启动失败时，查看 `adb start-server` 的诊断，并确认服务能监听 `127.0.0.1:5037`。
- `unauthorized` 表示等待设备授权。解锁设备并接受「允许 USB 调试」提示，随后重新查询。
- `offline` 表示设备已被 ADB 发现，但当前无法通信。重新接入 USB 后等状态恢复为 `device`。

将下列文件放在调用 Go 核心的可执行文件旁。缺少 OCR 文件只会影响需要识别屏幕文字的步骤。

| 文件 | 来源与用途 |
| --- | --- |
| `u2.jar` | [uiautomator2 JAR 0.4.0](https://github.com/openatx/android-uiautomator-server-jar/releases/tag/0.4.0)。运行时在设备上首次安装，供控件树、手势和截图使用。 |
| `app-uiautomator.apk` | [AdbKeyboard 2.4.0](https://github.com/openatx/android-uiautomator-server/releases/tag/2.4.0)。首次替换文本时按需安装，执行结束后恢复原输入法。已安装的输入法可以直接使用。 |
| `onnxruntime.dll` | [ONNX Runtime 1.23.2](https://www.nuget.org/packages/Microsoft.ML.OnnxRuntime/1.23.2)。取与程序架构一致的原生库，例如 NuGet 包中的 `runtimes/win-x64/native/onnxruntime.dll`。 |
| `libonnxruntime.dylib` | macOS Apple Silicon 用 [ONNX Runtime 1.23.2 ARM64 wheel](https://pypi.org/project/onnxruntime/1.23.2/#onnxruntime-1.23.2-cp313-cp313-macosx_13_0_arm64.whl)。提取 `onnxruntime/capi/libonnxruntime.1.23.2.dylib` 并改名为 `libonnxruntime.dylib`。该库的 Mach-O 最低系统版本为 macOS 13.4。 |
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

`Run` 拒绝开始时返回错误和空记录。开始后返回执行记录，结果在 `record.Result`，操作错误在 `record.Error`，每步的结束图像在 `record.Steps[i].Screenshot`。同一宿主用户下，各 Gnosis 进程以设备序列号共享占用锁。

Gnosis 启动 uiautomator2 后，在执行期间保持启动它的 ADB 连接。结束时先关闭监听，再核对启动时记录的 PID 和进程参数，结束自己启动的 JVM 并释放 UiAutomation。设备端 JAR 和输入法会保留。

如果设备返回 `INSTALL_FAILED_USER_RESTRICTED`，输入步骤会停止，并在执行记录中保留系统提示。需要由设备使用者按系统要求授权安装，或在已经安装 AdbKeyboard 的设备上运行输入步骤。
