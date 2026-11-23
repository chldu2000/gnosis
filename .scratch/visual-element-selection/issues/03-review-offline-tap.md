# 03 断开设备后复核坐标点击并独立重跑

Status: resolved

Blocked by: [02](02-edit-with-locator.md)、[AI 草稿工作台](../../ai-assisted-case-authoring/issues/03-review-in-workbench.md)

[所属规格](../spec.md)

将同一个选择器接到草稿的相应轨迹轮次。

## 验收条件

- [x] 会话中保存对应轮次的动作前截图、层级和尺寸，动作后的详情截图独立可读。
- [x] 断开设备，从原列表选中商品并替换坐标；缺少动作前附件时提示原因，原报告仍可读。
- [x] 将商品移到不同列表行，重新连接后无模型重跑，详情上的原断言通过。

## Comments

Windows 工作台在没有设备客户端的条件下，从磁盘打开同一次试跑的动作后截图及原断言，再从动作前的列表截图和树把坐标改为商品定位器。测试期间手机保持 USB 连接。目标换到第二行后公开 CLI 无模型重跑通过；另一组测试删除动作前图片后仍可读取原报告。见[真机验收](../../../cmd/gnosis-desktop/android_pilot_windows_test.go)和[缺失附件场景](../../../cmd/gnosis-desktop/picker_windows_test.go)。
