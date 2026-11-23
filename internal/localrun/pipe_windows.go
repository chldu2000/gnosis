package localrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"gnosis"
)

const pipeURL = "http://gnosis.local"

func userSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

func pipeName(sid string) string { return `\\.\pipe\gnosis-execution-v1-` + sid }

func HostDir() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "gnosis"), nil
}

func Serve() error {
	sid, err := userSID()
	if err != nil {
		return err
	}
	listener, err := winio.ListenPipe(pipeName(sid), &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")"})
	if err != nil {
		return err
	}
	defer listener.Close()
	root, err := HostDir()
	if err != nil {
		return err
	}
	pauseLimit := 30 * time.Minute
	if configured := os.Getenv("GNOSIS_PAUSE_LIMIT"); configured != "" {
		pauseLimit, err = time.ParseDuration(configured)
		if err != nil || pauseLimit <= 0 {
			return errors.New("GNOSIS_PAUSE_LIMIT must be a positive duration")
		}
	}
	manager, err := OpenManager(Config{
		StatePath: filepath.Join(root, "execution.json"), ReportDir: filepath.Join(root, "reports"), PauseLimit: pauseLimit,
		Model:      ReadModel,
		OpenDevice: func(serial string) (gnosis.Device, error) { return gnosis.NewAndroidDevice(serial) },
	})
	if err != nil {
		return err
	}
	return serveUntilIdle(listener, manager)
}

func serveUntilIdle(listener net.Listener, manager *Manager) error {
	var mu sync.Mutex
	connections := make(map[net.Conn]struct{})
	stopping := false
	server := &http.Server{
		Handler: endpoints(manager), ReadHeaderTimeout: 4 * time.Second,
		ConnState: func(conn net.Conn, state http.ConnState) {
			mu.Lock()
			defer mu.Unlock()
			switch state {
			case http.StateNew:
				if stopping {
					_ = conn.Close()
					return
				}
				connections[conn] = struct{}{}
			case http.StateClosed, http.StateHijacked:
				delete(connections, conn)
			}
		},
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		started := time.Now()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				mu.Lock()
				stopping = time.Since(started) >= 4*time.Second && len(connections) == 0 && manager.Idle()
				stop := stopping
				mu.Unlock()
				if stop {
					_ = server.Close()
					return
				}
			}
		}
	}()
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func endpoints(manager *Manager) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /capture", func(w http.ResponseWriter, r *http.Request) {
		device, err := manager.config.OpenDevice(r.URL.Query().Get("device"))
		if err != nil {
			writeResponse(w, nil, err)
			return
		}
		capture, err := gnosis.InspectDevice(r.Context(), device)
		writeResponse(w, capture, err)
	})
	mux.HandleFunc("GET /execution", func(w http.ResponseWriter, r *http.Request) {
		response, err := manager.Get(r.URL.Query().Get("id"), r.URL.Query().Get("request"))
		writeResponse(w, response, err)
	})
	mux.HandleFunc("GET /watch", func(w http.ResponseWriter, r *http.Request) {
		after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		if err != nil {
			writeResponse(w, nil, errors.New("invalid event sequence"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		response, err := manager.Watch(ctx, r.URL.Query().Get("id"), after)
		if errors.Is(err, context.DeadlineExceeded) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeResponse(w, response, err)
	})
	mux.HandleFunc("GET /devices", func(w http.ResponseWriter, r *http.Request) {
		devices, err := gnosis.AndroidDevices(r.Context())
		writeResponse(w, devices, err)
	})
	mux.HandleFunc("POST /start", func(w http.ResponseWriter, r *http.Request) {
		var request StartRequest
		if !readRequest(w, r, &request) {
			return
		}
		response, err := manager.Start(request)
		writeResponse(w, response, err)
	})
	mux.HandleFunc("POST /control", func(w http.ResponseWriter, r *http.Request) {
		var request struct{ ID, Command string }
		if !readRequest(w, r, &request) {
			return
		}
		response, err := manager.Control(request.ID, request.Command)
		// If normal completion has already won the race, report its final
		// state once the remaining cleanup and report writes are finished.
		for err == nil && request.Command == "cancel" && response.Progress.State == gnosis.ExecutionFinishing {
			response, err = manager.Watch(r.Context(), response.ID, response.Progress.Sequence)
		}
		writeResponse(w, response, err)
	})
	return mux
}

func readRequest(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeResponse(w, nil, errors.New("invalid request"))
		return false
	}
	return true
}

type responseError struct{ Message string }

func writeResponse(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrUnknownExecution) {
			status = http.StatusNotFound
		}
		w.WriteHeader(status)
		value = responseError{Message: err.Error()}
	}
	_ = json.NewEncoder(w).Encode(value)
}

type Client struct {
	http *http.Client
}

func NewClient() (*Client, error) {
	sid, err := userSID()
	if err != nil {
		return nil, err
	}
	name := pipeName(sid)
	return &Client{http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			ctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
			defer cancel()
			return winio.DialPipeContext(ctx, name)
		},
	}}}, nil
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

// Connect first checks for an existing host and launches the desktop binary in
// host mode only when the local pipe cannot be reached.
func (c *Client) Connect(ctx context.Context) (Snapshot, error) {
	latest, err := c.Get(ctx, "", "")
	if err == nil || ctx.Err() != nil {
		return latest, err
	}
	executable, err := os.Executable()
	if err != nil {
		return Snapshot{}, err
	}
	cmd := exec.Command(executable, "--execution-host")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS, HideWindow: true}
	if err := cmd.Start(); err != nil {
		return Snapshot{}, err
	}
	_ = cmd.Process.Release()
	ticker := time.NewTicker(125 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return Snapshot{}, fmt.Errorf("connect to local execution process: %w", ctx.Err())
		case <-ticker.C:
			latest, err = c.Get(ctx, "", "")
			if err == nil {
				return latest, nil
			}
		}
	}
}

func (c *Client) Get(ctx context.Context, id, request string) (Snapshot, error) {
	var result Snapshot
	err := c.call(ctx, http.MethodGet, "/execution?"+url.Values{"id": {id}, "request": {request}}.Encode(), nil, &result)
	return result, err
}

func (c *Client) Watch(ctx context.Context, id string, after uint64) (Snapshot, error) {
	var result Snapshot
	query := url.Values{"id": {id}, "after": {strconv.FormatUint(after, 10)}}
	err := c.call(ctx, http.MethodGet, "/watch?"+query.Encode(), nil, &result)
	return result, err
}

func (c *Client) Devices(ctx context.Context) ([]gnosis.DeviceInfo, error) {
	var result []gnosis.DeviceInfo
	err := c.call(ctx, http.MethodGet, "/devices", nil, &result)
	return result, err
}

func (c *Client) Capture(ctx context.Context, device string) (*gnosis.InterfaceCapture, error) {
	var result gnosis.InterfaceCapture
	err := c.call(ctx, http.MethodGet, "/capture?"+url.Values{"device": {device}}.Encode(), nil, &result)
	return &result, err
}

func (c *Client) Start(ctx context.Context, request StartRequest) (Snapshot, error) {
	var result Snapshot
	err := c.call(ctx, http.MethodPost, "/start", request, &result)
	return result, err
}

func (c *Client) Control(ctx context.Context, id, command string) (Snapshot, error) {
	var result Snapshot
	err := c.call(ctx, http.MethodPost, "/control", struct{ ID, Command string }{id, command}, &result)
	return result, err
}

// Rejected is a refusal received from the execution process. A transport
// failure after Start leaves acceptance unknown until the request is queried.
type Rejected struct{ Message string }

func (e *Rejected) Error() string { return e.Message }

func (c *Client) call(ctx context.Context, method, path string, request, result any) error {
	var payload []byte
	if request != nil {
		var err error
		payload, err = json.Marshal(request)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, pipeURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode == http.StatusNotFound {
		var diagnostic responseError
		_ = json.NewDecoder(resp.Body).Decode(&diagnostic)
		return ErrUnknownExecution
	}
	if resp.StatusCode != http.StatusOK {
		var diagnostic responseError
		if err := json.NewDecoder(resp.Body).Decode(&diagnostic); err != nil {
			return err
		}
		return &Rejected{Message: diagnostic.Message}
	}
	return json.NewDecoder(resp.Body).Decode(result)
}
