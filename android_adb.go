package gnosis

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// adbTransport uses the ADB server's smart socket protocol. A device
// connection is always selected by serial before requesting a service.
type adbTransport struct {
	address string
	serial  string
}

func (a adbTransport) open(ctx context.Context, service string, onDevice bool) (conn net.Conn, err error) {
	conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", a.address)
	if err != nil {
		return nil, err
	}
	socket := conn
	defer func() {
		if err != nil {
			socket.Close()
		}
	}()
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		if err := socket.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}
	if onDevice {
		if err := adbRequest(socket, "host:transport:"+a.serial); err != nil {
			return nil, err
		}
	}
	if err := adbRequest(socket, service); err != nil {
		return nil, err
	}
	return socket, nil
}

func adbRequest(socket io.ReadWriter, service string) error {
	if len(service) > 0xffff {
		return errors.New("ADB service name exceeds protocol limit")
	}
	if _, err := fmt.Fprintf(socket, "%04x%s", len(service), service); err != nil {
		return err
	}
	var status [4]byte
	if _, err := io.ReadFull(socket, status[:]); err != nil {
		return err
	}
	switch string(status[:]) {
	case "OKAY":
		return nil
	case "FAIL":
		message, err := adbReadString(socket)
		if err != nil {
			return err
		}
		return fmt.Errorf("ADB %s: %s", strings.SplitN(service, ":", 2)[0], message)
	default:
		return fmt.Errorf("unrecognized ADB status %q", status)
	}
}

func adbReadString(r io.Reader) (string, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return "", err
	}
	size, err := strconv.ParseUint(string(header[:]), 16, 16)
	if err != nil {
		return "", err
	}
	data := make([]byte, size)
	_, err = io.ReadFull(r, data)
	return string(data), err
}

func (a adbTransport) online() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := a.open(ctx, "host-serial:"+a.serial+":get-state", false)
	if err != nil {
		return false
	}
	defer conn.Close()
	state, err := adbReadString(conn)
	return err == nil && state == "device"
}

func (a adbTransport) dialAutomation(ctx context.Context, _, _ string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := a.open(ctx, "tcp:9008", true)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// shell is restricted to private provisioning commands in the adapter.
func (a adbTransport) shell(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := a.open(ctx, "shell:"+command, true)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	output, err := io.ReadAll(io.LimitReader(conn, 64<<10))
	return strings.TrimSpace(string(output)), err
}

func (a adbTransport) push(target string, source io.Reader) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn, err := a.open(ctx, "sync:", true)
	if err != nil {
		return err
	}
	defer conn.Close()

	path := []byte(target + ",33188") // regular file, mode 0644
	if err := adbSyncSend(conn, "SEND", uint32(len(path)), path); err != nil {
		return err
	}
	buffer := make([]byte, 64<<10)
	for {
		n, readErr := source.Read(buffer)
		if n > 0 {
			if err := adbSyncSend(conn, "DATA", uint32(n), buffer[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := adbSyncSend(conn, "DONE", uint32(time.Now().Unix()), nil); err != nil {
		return err
	}

	var reply [8]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return err
	}
	switch string(reply[:4]) {
	case "OKAY":
		return nil
	case "FAIL":
		message := make([]byte, min(binary.LittleEndian.Uint32(reply[4:]), 64<<10))
		if _, err := io.ReadFull(conn, message); err != nil {
			return err
		}
		return fmt.Errorf("ADB push: %s", message)
	default:
		return fmt.Errorf("unrecognized ADB sync status %q", reply[:4])
	}
}

func adbSyncSend(w io.Writer, command string, value uint32, data []byte) error {
	var header [8]byte
	copy(header[:4], command)
	binary.LittleEndian.PutUint32(header[4:], value)
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(data) > 0 {
		_, err := w.Write(data)
		return err
	}
	return nil
}
