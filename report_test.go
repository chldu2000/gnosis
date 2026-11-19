package gnosis_test

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gnosis"
)

func readReport(t *testing.T, path string) string {
	t.Helper()
	if path == "" || !filepath.IsAbs(path) {
		t.Fatalf("expected an absolute report path, got %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func requireInOrder(t *testing.T, text string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		i := strings.Index(text, fragment)
		if i < 0 {
			t.Fatalf("missing or out of order: %q", fragment)
		}
		text = text[i+len(fragment):]
	}
}

func checkReportImage(t *testing.T, report, relative string, frame int) {
	t.Helper()
	body := readReport(t, report)
	if !strings.Contains(body, `href="`+relative+`"`) || strings.Contains(body, "data:image/") {
		t.Fatalf("report does not link to the external image %s", relative)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(report), filepath.FromSlash(relative)))
	if err != nil || !bytes.Equal(data, fixtureScreenshot(frame)) {
		t.Fatalf("attachment %s does not contain frame %d: %v", relative, frame, err)
	}
}

func TestRunSavesAnOfflineReportWithoutChangingTheCase(t *testing.T) {
	assets, reports := t.TempDir(), t.TempDir()
	source := filepath.Join(assets, "case.yaml")
	const original = "name: '设置 <测试>'\nsteps:\n  - name: first step\n    wait: {duration: 0ms}\n  - wait: {duration: 0ms}\n"
	if err := os.WriteFile(source, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	testCase, _, err := gnosis.Load(source)
	if err != nil {
		t.Fatal(err)
	}
	d := &imageDevice{testDevice: &testDevice{}}
	clock := &testClock{now: time.Date(2026, 8, 4, 15, 52, 0, 0, time.UTC)}
	first, err := gnosis.Run(testCase, d, clock, gnosis.WithReports(reports))
	if err != nil || first.Result != gnosis.ResultExecuted {
		t.Fatalf("Run = %+v, %v", first, err)
	}
	body := readReport(t, first.ReportPath)
	requireInOrder(t, body, "设置 &lt;测试&gt;", d.ID(), clock.now.Format(time.RFC3339), "executed", "first step", "步骤 2")
	checkReportImage(t, first.ReportPath, "attachments/case-0001-step-0001.png", 1)
	checkReportImage(t, first.ReportPath, "attachments/case-0001-step-0002.png", 2)
	if strings.Contains(body, "设置 <测试>") || strings.Contains(body, "<script src=") || strings.Contains(body, "https://") {
		t.Fatal("report contains raw input or depends on an outside resource")
	}
	if relative, err := filepath.Rel(reports, first.ReportPath); err != nil || strings.HasPrefix(relative, "..") {
		t.Fatalf("report was not saved under its configured directory: %s", first.ReportPath)
	}

	d.frames = 0
	second, err := gnosis.Run(testCase, d, clock, gnosis.WithReports(reports))
	if err != nil || second.ReportPath == first.ReportPath || !second.StartedAt.Equal(first.StartedAt) {
		t.Fatalf("repeated execution reused its report or changed the clock: %+v, %v", second, err)
	}
	readReport(t, second.ReportPath)
	if readReport(t, first.ReportPath) != body {
		t.Fatal("repeated execution overwrote its earlier report")
	}
	saved, err := os.ReadFile(source)
	if err != nil || string(saved) != original {
		t.Fatal("execution modified the case")
	}
}

func TestRunReportsActualResultsAndOnlyStartedSteps(t *testing.T) {
	missing := &gnosis.Locator{ID: locatorString("missing")}
	for _, test := range []struct {
		name, fail string
		first      gnosis.Step
		want       gnosis.Result
		count      int
	}{
		{"pass", "", gnosis.Step{Expect: &gnosis.Assertion{Absent: missing}}, gnosis.ResultPass, 2},
		{"fail", "", gnosis.Step{Expect: &gnosis.Assertion{Exists: missing}}, gnosis.ResultFail, 1},
		{"blocked", "", gnosis.Step{Precondition: &gnosis.Assertion{Exists: missing}}, gnosis.ResultBlocked, 1},
		{"device error", "home", gnosis.Step{Key: &gnosis.Key{Name: "home"}}, gnosis.ResultError, 1},
		{"screenshot error", "screenshot", gnosis.Step{Wait: &gnosis.Wait{}}, gnosis.ResultError, 1},
		{"prepare error", "install", gnosis.Step{Wait: &gnosis.Wait{}}, gnosis.ResultError, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := &imageDevice{testDevice: &testDevice{fail: test.fail, fault: errors.New("<fixture error>")}}
			caseValue := gnosis.Case{Name: "case", Steps: []gnosis.Step{test.first, {Name: "later step", Wait: &gnosis.Wait{}}}}
			record, err := gnosis.Run(caseValue, d, &testClock{}, gnosis.WithReports(t.TempDir()))
			if err != nil || record.Result != test.want || len(record.Steps) != test.count {
				t.Fatalf("Run = %+v, %v", record, err)
			}
			body := readReport(t, record.ReportPath)
			if !strings.Contains(body, `data-status="`+string(test.want)+`"`) ||
				strings.Contains(body, "later step") != (test.count == 2) ||
				strings.Contains(body, "没有步骤记录") != (test.count == 0) ||
				strings.Contains(body, "截图不可用") != (test.fail == "screenshot") {
				t.Fatalf("unexpected report outcome: %s", body)
			}
			if test.want == gnosis.ResultError && (!strings.Contains(body, "&lt;fixture error&gt;") || strings.Contains(body, "<fixture error>")) {
				t.Fatal("error diagnostic was not safely rendered")
			}
		})
	}
}

func TestRunReportContainsTheAssertionJudgmentFrame(t *testing.T) {
	d := &imageDevice{testDevice: &testDevice{regions: [][]gnosis.TextRegion{nil, {{Text: "visible"}}}}}
	testCase := gnosis.Case{Steps: []gnosis.Step{{Expect: &gnosis.Assertion{
		ScreenText: &gnosis.ScreenText{Text: "visible"}, Timeout: time.Second,
	}}}}
	record, err := gnosis.Run(testCase, d, &testClock{}, gnosis.WithReports(t.TempDir()))
	if err != nil || record.Result != gnosis.ResultPass || d.frames != 2 {
		t.Fatalf("Run = %+v, %v, frames=%d", record, err, d.frames)
	}
	checkReportImage(t, record.ReportPath, "attachments/case-0001-step-0001.png", 2)
	images, err := os.ReadDir(filepath.Join(filepath.Dir(record.ReportPath), "attachments"))
	if err != nil || len(images) != 1 {
		t.Fatalf("report also saved polling frames: %v, %v", images, err)
	}
}

func TestRunDoesNotPublishForRefusalOrFailedSave(t *testing.T) {
	root := filepath.Join(t.TempDir(), "reports")
	d := &imageDevice{testDevice: &testDevice{offline: true}}
	record, err := gnosis.Run(gnosis.Case{}, d, &testClock{}, gnosis.WithReports(root))
	if record != nil || !errors.Is(err, gnosis.ErrDeviceOffline) {
		t.Fatalf("Run = %+v, %v", record, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("a refused execution created a report directory")
	}

	if err := os.WriteFile(root, []byte("existing file"), 0600); err != nil {
		t.Fatal(err)
	}
	d.offline = false
	record, err = gnosis.Run(gnosis.Case{}, d, &testClock{}, gnosis.WithReports(root))
	if err == nil || !strings.Contains(err.Error(), "save report") ||
		record == nil || record.Result != gnosis.ResultExecuted || record.ReportPath != "" || d.occupied {
		t.Fatalf("Run = %+v, %v, occupied=%v", record, err, d.occupied)
	}
	record, err = gnosis.Run(gnosis.Case{}, d, &testClock{}, gnosis.WithReports(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	readReport(t, record.ReportPath)
}

func TestRunReportKeepsPartialTrajectoryAndIMEAnnotation(t *testing.T) {
	model := startLocalModel(t, func(round int, w http.ResponseWriter, _ *http.Request) {
		switch round {
		case 1:
			writeDecision(w, `{"action":"type","params":{"text":"<input>"},"note":"  <model note>\n保留原文  "}`)
		case 2:
			writeDecision(w, `{"action":"wait","params":{"duration":"0ms"},"note":"second note"}`)
		default:
			t.Error("no more rounds expected")
		}
	})
	d := &imageDevice{testDevice: &testDevice{
		changeIME: true, fail: "restore", fault: errors.New("cannot restore " + testAPIKey),
	}, failFrame: 3}
	testCase := gnosis.Case{Name: "case " + testAPIKey, Steps: []gnosis.Step{withAI("work", 3)}}
	record, err := gnosis.Run(testCase, d, &testClock{}, gnosis.WithModel(model.config), gnosis.WithReports(t.TempDir()))
	if err != nil || record.Result != gnosis.ResultError || len(record.Steps[0].Trajectory) != 2 || record.IMENote == "" {
		t.Fatalf("Run = %+v, %v", record, err)
	}
	raw := readReport(t, record.ReportPath)
	requireInOrder(t, raw, "输入法注明", "cannot restore [redacted]", "截图不可用",
		"AI 轨迹", "<span>type</span>", "&lt;input&gt;", "  &lt;model note&gt;\n保留原文  ",
		"<span>wait</span>", "second note", "截图不可用")
	checkReportImage(t, record.ReportPath, "attachments/case-0001-step-0001-turn-0001.png", 2)
	if strings.Contains(raw, testAPIKey) || strings.Contains(raw, "raw-response-marker") ||
		strings.Contains(raw, "Bearer ") || strings.Contains(raw, "<model note>") || strings.Contains(raw, "<input>") {
		t.Fatal("report leaked request details, rendered model text as markup, or used the wrong frame")
	}
}

func TestRunReportShowsStepAndTrajectoryEndsAndOmitsEmptyTrajectory(t *testing.T) {
	model := scriptedModel(t, `{"action":"key","params":{"name":"back"},"note":"go back"}`, `{"stop":true}`)
	d := &imageDevice{testDevice: &testDevice{}}
	record, err := gnosis.Run(gnosis.Case{Steps: []gnosis.Step{withAI("go back", 1), withAI("stop", 1)}},
		d, &testClock{}, gnosis.WithModel(model.config), gnosis.WithReports(t.TempDir()))
	if err != nil || record.Result != gnosis.ResultExecuted {
		t.Fatalf("Run = %+v, %v", record, err)
	}
	body := readReport(t, record.ReportPath)
	if strings.Count(body, "<summary>AI 轨迹") != 1 {
		t.Fatal("report did not distinguish the step and trajectory images")
	}
	checkReportImage(t, record.ReportPath, "attachments/case-0001-step-0001.png", 2)
	checkReportImage(t, record.ReportPath, "attachments/case-0001-step-0001-turn-0001.png", 2)
	checkReportImage(t, record.ReportPath, "attachments/case-0001-step-0002.png", 3)
}
