package gnosis

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	androidName      = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)*$`)
	androidClass     = regexp.MustCompile(`^\.?[a-zA-Z_$][a-zA-Z0-9_$]*(\.[a-zA-Z_$][a-zA-Z0-9_$]*)*$`)
	keyboardAccepted = regexp.MustCompile(`(?m)^Broadcast completed: result=-1(?:[ ,]|$)`)
)

func activityComponent(app, entry string) (string, bool) {
	if !androidName.MatchString(app) {
		return "", false
	}
	if pkg, class, found := strings.Cut(entry, "/"); found {
		if pkg != app {
			return "", false
		}
		entry = class
	}
	if !androidClass.MatchString(entry) {
		return "", false
	}
	return app + "/" + entry, true
}

// command is private. Callers pass only fixed Android operations and validated
// component names or Base64 text. Runtime.exec in uiautomator2 splits the
// arguments directly, without invoking a shell.
func (d *AndroidDevice) command(operation, command string) (string, error) {
	var result *struct {
		Stdout     string
		Stderr     string
		ReturnCode int
	}
	err := d.rpc("executeShellCommand", &result, 30*time.Second, command, 25000)
	if err != nil {
		return "", fmt.Errorf("%s: %w", operation, err)
	}
	if result == nil {
		return "", fmt.Errorf("%s: uiautomator2 returned no result", operation)
	}
	if result.ReturnCode != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			// Android's package manager may write installation failures to stdout.
			detail = strings.TrimSpace(result.Stdout)
		}
		if detail == "" {
			return "", fmt.Errorf("%s: Android exit %d", operation, result.ReturnCode)
		}
		return "", fmt.Errorf("%s: Android exit %d: %s", operation, result.ReturnCode, detail)
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (d *AndroidDevice) Launch(app, entry string) error {
	if !androidName.MatchString(app) {
		return fmt.Errorf("invalid Android application identifier %q", app)
	}
	if entry == "" {
		output, err := d.command("resolve app launch activity",
			"cmd package resolve-activity --brief -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -p "+app)
		if err != nil {
			return err
		}
		lines := strings.Split(output, "\n")
		entry = strings.TrimSpace(lines[len(lines)-1])
	}
	component, ok := activityComponent(app, entry)
	if !ok {
		return fmt.Errorf("no valid Android activity for %q and entry %q", app, entry)
	}
	output, err := d.command("launch app", "am start -W -n "+component)
	if err != nil {
		return err
	}
	if !slices.Contains(strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n"), "Status: ok") {
		return fmt.Errorf("Android did not start %s: %s", component, output)
	}
	return nil
}

func (d *AndroidDevice) ReplaceText(text string) (bool, error) {
	nodes, err := d.hierarchy()
	if err != nil {
		return false, err
	}
	if !visitAndroidNodes(nodes, func(node androidNode) bool { return node.Focused }) {
		return false, errors.New("no Android element has input focus")
	}
	current, err := d.currentIME()
	if err != nil {
		return false, err
	}
	changed := false
	if current != automationIME {
		if err := d.ensureKeyboard(); err != nil {
			return false, err
		}
		if _, err := d.command("enable automation input method", "ime enable "+automationIME); err != nil {
			return false, err
		}
		// If the switch is applied but its reply is lost, still attempt recovery.
		if d.restore == "" {
			d.restore = current
		}
		changed = true
		if err := d.setIME(automationIME); err != nil {
			return changed, err
		}
	}
	const action = "ADB_KEYBOARD_SET_TEXT"
	command := "am broadcast -a " + action + " -p com.github.uiautomator"
	if text != "" {
		command += " --es text " + base64.StdEncoding.EncodeToString([]byte(text))
	}
	output, err := d.command("replace focused text", command)
	if err != nil {
		return changed, err
	}
	if !keyboardAccepted.MatchString(output) || strings.Contains(output, `data="`+action+`"`) {
		return changed, errors.New("Android input method did not accept the replacement in the focused field")
	}
	return changed, nil
}

func (d *AndroidDevice) ensureKeyboard() error {
	list, err := d.command("list Android input methods", "ime list -s -a")
	if err != nil {
		return err
	}
	for _, component := range strings.Fields(list) {
		if component == automationIME {
			return nil
		}
	}
	if err := d.upload("app-uiautomator.apk", imeAPK); err != nil {
		return err
	}
	output, err := d.command("install automation input method", "pm install "+imeAPK)
	if err != nil {
		return err
	}
	if output != "Success" {
		return fmt.Errorf("install automation input method: %s", output)
	}
	return nil
}

func (d *AndroidDevice) currentIME() (string, error) {
	current, err := d.command("read current input method", "settings get secure default_input_method")
	if err != nil {
		return "", err
	}
	app, _, ok := strings.Cut(current, "/")
	if _, valid := activityComponent(app, current); !ok || !valid {
		return "", errors.New("could not identify the current Android input method")
	}
	return current, nil
}

func (d *AndroidDevice) setIME(component string) error {
	if _, err := d.command("switch input method", "ime set "+component); err != nil {
		return err
	}
	current, err := d.currentIME()
	if err != nil {
		return err
	}
	if current != component {
		return fmt.Errorf("Android input method is %q after switching to %q", current, component)
	}
	return nil
}

func (d *AndroidDevice) RestoreIME() error {
	original := d.restore
	// A suite retains the device between cases. Each restoration belongs
	// only to the case that changed the input method.
	d.restore = ""
	if original == "" {
		return nil
	}
	current, err := d.currentIME()
	if err != nil {
		return err
	}
	if current != original {
		return d.setIME(original)
	}
	return nil
}
