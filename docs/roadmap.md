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

Persist a report per started run (result, step conclusions, screenshots, trajectories). Suite runner: hold one device, continue after fail/blocked, stop after error, refuse to start when load would fail. CLI: require an explicit device, non-zero exit on fail/blocked/error and on runs that never start; print report path. Restore Android IME after the case result is fixed when the run changed it.

## 5. MyGo desktop forms

**Feature dir:** `.scratch/mygo-desktop/` (create when starting)

Native MyGo UI (`mygo_nowebview`): edit cases and suites via forms, pick a device, run, open the HTML report in the system browser, edit model settings. Same load and run core as the CLI. No embedded code editor, no WebView, no recording, no trajectory import.

## Out of first release

Trajectory-to-case export, recording, multi-device orchestration inside one process, non-Windows hosts, HarmonyOS versions that are not NEXT, license/auth.
