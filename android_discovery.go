package gnosis

import (
	"context"
	"strings"
	"time"
)

type DeviceInfo struct {
	Platform string
	ID       string
	Name     string
	State    string
}

// AndroidDevices lists the devices known to the local ADB server. State is
// "device" only when a device is online and authorized.
func AndroidDevices(ctx context.Context) ([]DeviceInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	adb := adbTransport{address: androidADBAddr}
	conn, err := adb.open(ctx, "host:devices-l", false)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	defer interruptADB(ctx, conn)()
	list, err := adbReadString(conn)
	if err != nil {
		return nil, err
	}
	return androidDeviceList(list), nil
}

func androidDeviceList(list string) []DeviceInfo {
	var devices []DeviceInfo
	for _, line := range strings.Split(list, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		device := DeviceInfo{Platform: "Android", ID: fields[0], State: fields[1]}
		for _, field := range fields[2:] {
			if name, ok := strings.CutPrefix(field, "model:"); ok {
				device.Name = name
				break
			}
		}
		devices = append(devices, device)
	}
	return devices
}
