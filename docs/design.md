# First-release design

Settled product and behaviour decisions for Gnosis v1. Terms live in [`GLOSSARY.md`](../GLOSSARY.md). Hard-to-reverse choices live in [`docs/adr/`](adr/). File-format detail for cases and suites lives in [`.scratch/case-and-suite-file-format/spec.md`](../.scratch/case-and-suite-file-format/spec.md). Build order lives in [`docs/roadmap.md`](roadmap.md).

## Product

- One Go core without UI. The MyGo desktop app uses the shared Go library for editing and validation and a local Gnosis process for execution control. The headless CLI keeps its existing invocation and uses the same core.
- Desktop UI is MyGo native, built with `mygo_nowebview`. Reports are static HTML opened in the system browser; no in-app WebView in v1.
- First host release is Windows only. Devices under test are Android and HarmonyOS NEXT (`hdc` + `uitest`). Older HarmonyOS that still uses adb is out of scope.
- No login, accounts, or license in v1. No recording. No export of AI trajectories back into cases.
- The desktop phase includes local start, pause, resume, cancel, and progress. A later Gnosis phase adds the connection from an execution instance to a separately implemented cloud project. See [Execution control and cloud integration](execution-control.md).

## Assets and editing

- A **case** is an ordered list of steps with a name. Names may repeat. One case per file (UTF-8 YAML). Devices and models are chosen at execution time, never stored in the case.
- A **suite** is another file that references cases by paths relative to the suite file. The same case may be referenced twice to run twice. Suites have no merged result of their own.
- Desktop editing uses native forms. The desktop asks for confirmation to save unsaved changes before executing a case or suite from disk. The same files can be run by the CLI. The suite editor retains relative case references, their order, and duplicates.
- Saving from a form preserves comments and omitted fields in unchanged parts of the YAML. Saving a suite to a new location recalculates its relative paths so the references still point to the same case files in the same order, including duplicates.
- If a file changed on disk after it was opened, the desktop stops the save and offers to reload the file or save the form to a different location. Before saving a suite to a new location, the desktop checks that each reference can be expressed as a relative path. If a relative path cannot be formed, for example when the destination is on a different Windows volume from a referenced case, the desktop identifies the reference and asks for a different destination before writing the file.
- Unknown fields, wrong types, and missing required content refuse to load, with file and location. Load never rewrites files. Duplicate YAML keys refuse.
- If a name is omitted, the case name is the filename without its last extension. Step names are optional; reports use 1-based indices when absent.

## Steps

Three kinds only: **action**, **assertion**, **AI step**. Assertions are their own steps in the sequence (not attached to actions). Mark each assertion as precondition or expect; there is no default.

### Actions (closed set)

Launch app (app id required; entry optional), tap (locator), long-press (locator; default 800 ms), tap-at (screen ratios), swipe (from/to ratios; default 300 ms), type text (replaces all text in the current focus; no append; no implied tap), key (back or home), wait (duration required, no default).

Ratio coordinates outside `[0, 1]` load successfully and become an **error** at execution. Script actions and AI-issued actions use the same set.

### Assertions (closed set)

Element exists, element absent, text equals, text contains, attribute equals (`enabled` or `checked` only), screen text visible (pixels / OCR). Default timeout 10 s; overridable per assertion. Assertions poll until true or the timeout ends.

- Locator miss: existence/text/attribute assertions have concluded (fail or block). Absence is true. Actions that need exactly one element: miss or ambiguous match without index → **error**.
- Attribute missing on a found element → assertion false (fail or block).
- Unreadable UI / failed screenshot / unavailable OCR engine → **error**, not a false assertion.
- OCR that runs but does not see the text → assertion false.

Display text and locator text come from the control tree (including off-screen nodes). Screen text comes from pixels. Locators search the tree first; OCR backup only on zero tree matches. Locator `text` is display text; `description` is the accessibility string. Index is 1-based and must be paired with at least one other condition at load time (see case-format spec).

A locator may narrow its target using a `relative` rule. Its `anchor` uses ordinary ID, text, description or type conditions and must resolve to one tree node. For `within`, ascend `levels` parents from the anchor (default zero) and search that container and its descendants, excluding the anchor. For `left_of`, `right_of`, `above` and `below`, compare disjoint bounds along the named axis and require overlap on the other axis. Target conditions may be omitted for a relative rule. Resolve both sides in the same tree at execution time. Apply an explicit target index after relative matching. A duplicate anchor is an error; a missing anchor or container prevents a click and leaves an assertion, including absence, unsatisfied until its timeout. OCR fallback applies only to ordinary text locators.

### AI steps

One goal string and an optional max-steps (default 20). The model may only request the action closed set. Voluntary stop or max-steps exhausted → step result **executed**; following expect assertions still run. Model unreachable, response outside the closed set, or a failed device operation → **error** and stop. Each trajectory turn keeps the device operation, a short model note (verbatim), and a screenshot. Full prompts and raw responses stay out of the report. Trajectory-to-case export is out of v1.

## Execution and results

Results: **pass**, **fail**, **blocked**, **error**, **executed**.

- Actions yield executed or error. Assertions yield pass, fail, or blocked. AI steps yield executed or error.
- Fail, blocked, and error stop immediately; the case result is that first terminal result.
- No expect assertions (including empty step list) → **executed**, not pass.
- One device per case or suite execution. One active execution per device and per process. A competing local start is refused before execution and reports that the device or process is busy. Parallel executions use separate processes.
- Suites run cases in order on one device and hold that device. Fail or blocked on a case → next case still starts. Error on a case → suite stops; cases not started have no result. Suite starts only after every referenced case loads; empty suite or unreadable reference → no run, no report, non-zero CLI exit.
- No auto-retry. No clearing apps or returning home before a run; leftover UI is a precondition problem.
- Android: ensure uiautomator2 is installed and keep it; do not uninstall after the run. If the run changed the IME, switch back after the case result is fixed; failure to switch back is noted in the report only. HarmonyOS NEXT uses system `uitest` and does not change IME.
- A controlled execution pauses at a safe point and keeps the device occupied. Cancellation stops further steps and preserves completed results. If cancellation interrupts a case before its result is determined, keep its completed step records and record the cancellation as an execution state. For Android, still attempt to restore the IME before releasing the device.
- Model endpoint, model name, and secret live in the host execution environment. CLI uses that by default and may override for one run. Secrets never appear in reports; model name and endpoint may.

## Reports

- Each started execution publishes one offline report in a session directory outside the case tree. Its `report.html` links to screenshots in the sibling `attachments/` directory. Copy the session directory to keep the report portable.
- The report lists cases in execution order, including repeated references and unstarted cases. Readers can search by case name, source file, or sequence number and filter by result. Each started case retains its own result, diagnostics, and step history.
- The same report is updated after each recorded case. Cancellation finalizes it with the execution state, completed steps and any already determined results. A saved checkpoint remains readable if execution is interrupted.
- Every finished step has an end-of-step screenshot. AI trajectory turns each have a screenshot. Assertion poll frames do not.
- Desktop automatically opens the saved report for a standalone case in the system browser. The execution view links to the session report and lists individual case results. The saved path is restored when the window reconnects. The CLI prints the saved report path once.
- No run started (bad files, empty suite, offline device) → no report.

## Host packaging notes

- Device I/O uses protocol libraries, not shipping `adb`/`hdc` executables as the primary path (CLI tools may remain a debug fallback).
- Screen-text OCR: ONNX Runtime + PP-OCR Chinese models beside the binary (not pure-Go OCR, not online vision APIs).
- AI executor is in-process Go against an OpenAI-compatible API; do not wrap Mobilerun.
