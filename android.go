package gnosis

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const (
	automationJar = "/data/local/tmp/u2.jar"
	imeAPK        = "/data/local/tmp/gnosis-uiautomator-ime.apk"
	automationIME = "com.github.uiautomator/.AdbKeyboard"
)

// AndroidDevice uses an already running local ADB server and selects the
// connected device by serial. Its operations are scoped to one Run at a time.
type AndroidDevice struct {
	adb               adbTransport
	claim             *flock.Flock
	http              *http.Client
	assetsDir         string
	ocr               androidOCR
	restore           string
	startedAutomation bool
}

var _ Device = (*AndroidDevice)(nil)

// NewAndroidDevice selects a serial from the local ADB server on port 5037.
// Provisioning and OCR assets are read next to the executable when needed.
func NewAndroidDevice(serial string) (*AndroidDevice, error) {
	if serial == "" || strings.ContainsAny(serial, " \r\n\t\x00") {
		return nil, errors.New("expected an Android device serial")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate Android assets: %w", err)
	}
	// Store the lock separately from the executable so different builds for
	// the same user contend for the same connected device.
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("locate device lock directory: %w", err)
	}
	dir = filepath.Join(dir, "gnosis", "devices")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create device lock directory: %w", err)
	}
	lock := filepath.Join(dir, fmt.Sprintf("android-%x.lock", sha256.Sum256([]byte(serial))))
	adb := adbTransport{address: "127.0.0.1:5037", serial: serial}
	transport := &http.Transport{
		DialContext:       adb.dialAutomation,
		DisableKeepAlives: true,
	}
	return &AndroidDevice{
		adb: adb, claim: flock.New(lock), assetsDir: filepath.Dir(executable),
		http: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (d *AndroidDevice) ID() string   { return d.adb.serial }
func (d *AndroidDevice) Online() bool { return d.adb.online() }

func (d *AndroidDevice) Occupied() bool {
	if d.claim.Locked() {
		return true
	}
	probe := flock.New(d.claim.Path())
	ok, err := probe.TryLock()
	if err != nil || !ok {
		return true
	}
	return probe.Unlock() != nil
}

func (d *AndroidDevice) TryOccupy() bool {
	ok, err := d.claim.TryLock()
	return err == nil && ok
}

func (d *AndroidDevice) Release() {
	d.ocr.close()
	d.restore = ""
	if d.startedAutomation {
		d.startedAutomation = false
		// Stop our listener while keeping the installed jar and keyboard.
		_, _ = d.automationGET("/stop")
	}
	d.http.CloseIdleConnections()
	_ = d.claim.Unlock()
}

func (d *AndroidDevice) EnsureAutomation() error {
	if d.automationReady() {
		return nil
	}
	installed, err := d.adb.shell("test -s " + automationJar + " && echo present")
	if err != nil {
		return err
	}
	if installed != "present" {
		// A failed upload must not leave a partial jar at the installation path.
		staging := automationJar + ".gnosis"
		if err := d.upload("u2.jar", staging); err != nil {
			return err
		}
		output, err := d.adb.shell("mv " + staging + " " + automationJar + " && echo installed")
		if err != nil || output != "installed" {
			return fmt.Errorf("install uiautomator2: %v %s", err, output)
		}
	}
	const launch = "CLASSPATH=" + automationJar + " nohup app_process / com.wetest.uia2.Main -p 9008 </dev/null >/data/local/tmp/gnosis-uiautomator.log 2>&1 &"
	if _, err := d.adb.shell(launch); err != nil {
		return fmt.Errorf("start uiautomator2: %w", err)
	}
	d.startedAutomation = true
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if d.automationReady() {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("uiautomator2 did not become ready on port 9008")
}

func (d *AndroidDevice) upload(name, target string) error {
	file, err := os.Open(filepath.Join(d.assetsDir, name))
	if err != nil {
		return fmt.Errorf("open Android asset %s: %w", name, err)
	}
	defer file.Close()
	if err := d.adb.push(target, file); err != nil {
		return fmt.Errorf("upload %s: %w", name, err)
	}
	return nil
}

func (d *AndroidDevice) automationReady() bool {
	body, err := d.automationGET("/ping")
	return err == nil && strings.TrimSpace(body) == "pong"
}

func (d *AndroidDevice) automationGET(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://uiautomator"+path, nil)
	if err != nil {
		return "", err
	}
	res, err := d.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("uiautomator2 %s: HTTP %d", path, res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 128))
	return string(body), err
}

func (d *AndroidDevice) rpc(method string, result any, timeout time.Duration, args ...any) error {
	if args == nil {
		args = []any{}
	}
	body, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
	}{"2.0", 1, method, args})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://uiautomator/jsonrpc/0", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("uiautomator2 %s: %w", method, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("uiautomator2 %s: HTTP %d", method, res.StatusCode)
	}
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(&response); err != nil {
		return fmt.Errorf("uiautomator2 %s response: %w", method, err)
	}
	if response.Error != nil {
		return fmt.Errorf("uiautomator2 %s: %d %s", method, response.Error.Code, response.Error.Message)
	}
	if len(response.Result) == 0 {
		return fmt.Errorf("uiautomator2 %s: missing result", method)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("uiautomator2 %s result: %w", method, err)
	}
	return nil
}

func (d *AndroidDevice) accepted(method string, duration time.Duration, args ...any) error {
	timeout := max(30*time.Second, min(duration, time.Duration(math.MaxInt64)-5*time.Second)+5*time.Second)
	var ok bool
	if err := d.rpc(method, &ok, timeout, args...); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("uiautomator2 rejected %s", method)
	}
	return nil
}

func (d *AndroidDevice) ScreenSize() (image.Point, error) {
	var info struct {
		Width  int `json:"displayWidth"`
		Height int `json:"displayHeight"`
	}
	if err := d.rpc("deviceInfo", &info, 30*time.Second); err != nil {
		return image.Point{}, err
	}
	if info.Width <= 0 || info.Height <= 0 {
		return image.Point{}, errors.New("uiautomator2 returned no screen dimensions")
	}
	return image.Pt(info.Width, info.Height), nil
}

type androidNode struct {
	ID          string        `xml:"resource-id,attr"`
	Text        string        `xml:"text,attr"`
	Description string        `xml:"content-desc,attr"`
	Class       string        `xml:"class,attr"`
	Bounds      string        `xml:"bounds,attr"`
	Enabled     *bool         `xml:"enabled,attr"`
	Checked     *bool         `xml:"checked,attr"`
	Focused     bool          `xml:"focused,attr"`
	Children    []androidNode `xml:"node"`
}

func (d *AndroidDevice) hierarchy() ([]androidNode, error) {
	var source string
	if err := d.rpc("dumpWindowHierarchy", &source, 30*time.Second, false, math.MaxInt32); err != nil {
		return nil, err
	}
	var hierarchy struct {
		XMLName xml.Name      `xml:"hierarchy"`
		Nodes   []androidNode `xml:"node"`
	}
	if err := xml.Unmarshal([]byte(source), &hierarchy); err != nil {
		return nil, fmt.Errorf("parse Android control tree: %w", err)
	}
	return hierarchy.Nodes, nil
}

func (d *AndroidDevice) ControlTree() ([]Element, error) {
	nodes, err := d.hierarchy()
	if err != nil {
		return nil, err
	}
	var elements []Element
	visitAndroidNodes(nodes, func(node androidNode) bool {
		e := Element{
			ID: node.ID, Text: node.Text, Description: node.Description, Type: node.Class,
			Enabled: node.Enabled, Checked: node.Checked,
		}
		var minX, minY, maxX, maxY int
		if n, _ := fmt.Sscanf(node.Bounds, "[%d,%d][%d,%d]", &minX, &minY, &maxX, &maxY); n == 4 {
			e.Bounds = image.Rectangle{Min: image.Pt(minX, minY), Max: image.Pt(maxX, maxY)}
		}
		elements = append(elements, e)
		return false
	})
	return elements, nil
}

func visitAndroidNodes(nodes []androidNode, visit func(androidNode) bool) bool {
	for _, node := range nodes {
		if visit(node) || visitAndroidNodes(node.Children, visit) {
			return true
		}
	}
	return false
}

func (d *AndroidDevice) Screenshot() ([]byte, error) {
	var encoded string
	if err := d.rpc("takeScreenshot", &encoded, 30*time.Second, 1, 100); err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("read screenshot dimensions: %w", err)
	}
	return data, nil
}

func (d *AndroidDevice) Tap(p image.Point) error {
	return d.accepted("click", 0, p.X, p.Y)
}

func (d *AndroidDevice) LongPress(p image.Point, duration time.Duration) error {
	return d.accepted("click", duration, p.X, p.Y, duration.Milliseconds())
}

func (d *AndroidDevice) Swipe(from, to image.Point, duration time.Duration) error {
	// UiAutomator expresses a swipe as 5 ms steps.
	steps := int64(math.Round(float64(duration) / float64(5*time.Millisecond)))
	if steps > math.MaxInt32 {
		return errors.New("swipe duration exceeds Android's limit")
	}
	return d.accepted("swipe", duration, from.X, from.Y, to.X, to.Y, max(1, steps))
}

func (d *AndroidDevice) PressKey(key string) error {
	if key != "back" && key != "home" {
		return fmt.Errorf("unsupported Android key %q", key)
	}
	return d.accepted("pressKey", 0, key)
}
