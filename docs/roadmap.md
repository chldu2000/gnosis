# Build order

Ordered delivery for first release. Do not start a later phase until the earlier phase's load/execute surface is usable by the next consumer. Specs and tickets live under `.scratch/<feature>/` per [`docs/agents/issue-tracker.md`](agents/issue-tracker.md). Behaviour authority is [`docs/design.md`](design.md) plus [`GLOSSARY.md`](../GLOSSARY.md) and [`docs/adr/`](adr/).

## 1. Case and suite file format and load

**Feature dir:** `.scratch/case-and-suite-file-format/`

Define YAML case and suite files and a single load entry that refuses unknown fields, wrong types, and missing required content without rewriting files. Defaults (assertion timeout, long-press/swipe durations, AI max-steps) are filled at load. No device or network.

This phase is the foundation for CLI and desktop; later phases consume the loaded case/suite, they do not re-parse YAML.

## 2. Android device layer and assertions

**Feature dir:** `.scratch/android-device-and-assertions/` (create when starting)

Go adb protocol + uiautomator2 for control tree, gestures, type, launch, screenshots. Install and keep uiautomator2. Run action and assertion steps on one Android device: polling timeouts, locator resolve (tree then OCR backup), result stop rules, end-of-step screenshots into a run record. ONNX PP-OCR for screen-text assertions and locator backup. Still no AI loop and no suite orchestration beyond what load already returns.

HarmonyOS NEXT device adapter can follow in the same phase or immediately after Android when the shared device-operation surface is stable; both platforms must share the same operation and locator shapes from design.

## 3. AI steps

**Feature dir:** `.scratch/ai-steps/` (create when starting)

In-process agent loop: control tree + screenshot → OpenAI-compatible model → closed-set device operations only. Max-steps / voluntary stop → executed; bad model or failed operation → error. Trajectory turns with short notes and screenshots. Model settings from host execution environment; secrets never in reports.

## 4. Reports and suite runs

**Feature dir:** `.scratch/reports-and-suite-runs/` (create when starting)

Persist one report per started execution with searchable case results, step records and relative screenshot attachments. Suite runner: hold one device, continue after fail/blocked, stop after error, refuse to start when load would fail. CLI: require an explicit device, non-zero exit on fail/blocked/error and on runs that never start; print one report path. Restore Android IME after the case result is fixed when the run changed it.

## 5. MyGo desktop forms

**Feature dir:** `.scratch/mygo-desktop/` (create when starting)

Native MyGo UI (`mygo_nowebview`): edit cases and suites via forms, pick a device, open the HTML report in the system browser, and edit model settings. First add local start, pause, resume, cancel, and progress to the shared Go core and connect the desktop app to an independent local Gnosis process over a Windows named pipe. Closing the window leaves the execution with that process. The CLI keeps its existing invocation. The desktop uses native forms and the system browser for reports.

## After the first release · Connect Gnosis instances to cloud dispatch

Add the instance side in this repository after local execution control is verified. It connects to the cloud, advertises available devices, validates task packages, applies control commands, and reports execution progress. A separate project implements the cloud scheduler, task storage, and operator interface. See [Execution control and cloud integration](execution-control.md) for the shared contract.

## After the first release · AI-assisted case authoring

**Feature dir:** `.scratch/ai-assisted-case-authoring/` (create when starting)

This milestone can proceed independently of cloud dispatch and macOS desktop work. Start with shared Go logic and the CLI, then add the workflow to the Windows workbench.

1. **Generate and review a draft.** Use the structured record of a just-completed, passing standalone case execution and the case version loaded at its start. The case must contain at least one AI step, and every AI step must have recorded at least one action. Keep the original actions and assertions in order and replace each AI step with its recorded actions and parameters. Flag coordinate taps for the author to review. After review, the author can explicitly save the draft as a new case in the existing YAML format and add it to a suite through the existing workflow.
2. **Verify on an Android device through the CLI.** Run the saved case again from the same starting state with no model configured and verify that it passes. Prepare a second test state in which the same actions can run but the expected condition is false. Verify that the expected assertion fails and is recorded in the report.
3. **Connect the Windows workbench.** Open the draft in the existing editor for review and Save As. Put the controls that start a trial execution or rerun at the top of the right-hand execution pane. Each execution uses the existing session report and attachment layout. Use [visual element selection](#after-the-first-release--visual-element-selection) to review flagged coordinate taps when the picker is available.

For the pilot, compare manual and AI-assisted authoring on the same Android scenario. Measure elapsed time through the first independent passing rerun, including time spent reviewing actions and correcting locators or assertions. Record the starting screen, account and test data so both reruns exercise the intended path. During draft review, show the original goal alongside the recorded actions, screenshots and expected assertion. For example, selecting a search-history entry can produce the expected result while bypassing the search button under test. See the [AI step authoring guidance](ai-execution.md) and [CLI regression workflow](cli-execution.md).

## After the first release · Visual element selection

**Feature dir:** `.scratch/visual-element-selection/` (create when starting)

Deliver manual element selection for Android devices in the Windows workbench as an independent milestone. An author can use it with a new case before configuring a model or running an AI step. CLI draft generation can ship independently. Both editing workflows share the screenshot, hierarchical control tree with element bounds, screen dimensions, locator matching logic and existing step form.

1. **Select an element from a connected device.** The author navigates on the device and refreshes the view on demand through the local execution process, respecting device ownership. Selecting an element in the screenshot highlights its node in the tree and vice versa. Show its attributes, candidate locators and the number of elements each locator matches in that capture. Let the author fill a locator in an existing step or create an action or assertion. Verify that selecting a notification switch leaves its state untouched while the author writes a tap or chooses the expected `checked` value.
2. **Review an AI coordinate tap offline.** For each AI coordinate tap that needs review, save its pre-action screenshot, hierarchical control tree and screen dimensions in the execution session, linked to the corresponding trajectory turn. Keep screenshots as file attachments. The same picker opens that observation after the device disconnects. The author can replace the coordinates in the draft with a locator and use the post-action screenshot to check where the tap led.

For acceptance, use a passing case in which an AI coordinate tap opens a product from a list. Disconnect the device and use the saved list view to replace the tap with a locator. Reconnect, move the product to a different row, then rerun the saved case with no model configured. The existing assertion must identify the intended product on the detail screen.

## Out of first release

Trajectory-to-case export, recording, embedded code editor, WebView, multi-device orchestration inside one process, non-Windows hosts, HarmonyOS versions that are not NEXT, license/auth, and cloud dispatch integration.
