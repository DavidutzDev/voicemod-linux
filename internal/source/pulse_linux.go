//go:build linux

package source

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// pulseSource is a module-pipe-source managed via the `pactl` CLI.
type pulseSource struct {
	cfg      Config
	moduleID string
	fifo     *os.File
	ownFifo  bool // we created the FIFO and should remove it on Close
}

// Register creates the FIFO (if needed), loads a module-pipe-source pointed at
// it, and opens the FIFO for writing. Order matters: the module must be loaded
// (it opens the read end) before we open the write end, otherwise the open
// blocks forever.
func Register(cfg Config) (Sink, error) {
	if _, err := exec.LookPath("pactl"); err != nil {
		return nil, errors.New("pactl not found: install pipewire-pulse or pulseaudio-utils")
	}

	fifoPath := cfg.fifo()
	ownFifo, err := ensureFifo(fifoPath)
	if err != nil {
		return nil, err
	}

	id, err := loadModule(cfg, fifoPath)
	if err != nil {
		if ownFifo {
			_ = os.Remove(fifoPath)
		}
		return nil, err
	}

	// Module is loaded and holds the read end open, so this won't block.
	f, err := os.OpenFile(fifoPath, os.O_WRONLY, os.ModeNamedPipe)
	if err != nil {
		_ = unloadModule(id)
		if ownFifo {
			_ = os.Remove(fifoPath)
		}
		return nil, fmt.Errorf("open fifo %s: %w", fifoPath, err)
	}

	return &pulseSource{cfg: cfg, moduleID: id, fifo: f, ownFifo: ownFifo}, nil
}

// Write feeds PCM to the virtual source. If the reader (the module) momentarily
// drops the pipe, it reopens once and retries rather than failing the stream.
func (s *pulseSource) Write(p []byte) (int, error) {
	n, err := s.fifo.Write(p)
	if err == nil || !errors.Is(err, syscall.EPIPE) {
		return n, err
	}
	_ = s.fifo.Close()
	f, e := os.OpenFile(s.cfg.fifo(), os.O_WRONLY, os.ModeNamedPipe)
	if e != nil {
		return n, e
	}
	s.fifo = f
	return s.fifo.Write(p)
}

// Close unloads the module and removes the FIFO we created. Idempotent enough
// for defer + signal-driven shutdown.
func (s *pulseSource) Close() error {
	var errs []error
	if s.fifo != nil {
		if err := s.fifo.Close(); err != nil {
			errs = append(errs, err)
		}
		s.fifo = nil
	}
	if s.moduleID != "" {
		if err := unloadModule(s.moduleID); err != nil {
			errs = append(errs, err)
		}
		s.moduleID = ""
	}
	if s.ownFifo {
		if err := os.Remove(s.cfg.fifo()); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ensureFifo makes the FIFO if absent. Returns whether we created it.
func ensureFifo(path string) (bool, error) {
	switch fi, err := os.Stat(path); {
	case err == nil:
		if fi.Mode()&os.ModeNamedPipe == 0 {
			return false, fmt.Errorf("%s exists and is not a FIFO", path)
		}
		return false, nil // reuse existing pipe
	case os.IsNotExist(err):
		if err := syscall.Mkfifo(path, 0o644); err != nil {
			return false, fmt.Errorf("mkfifo %s: %w", path, err)
		}
		return true, nil
	default:
		return false, err
	}
}

func loadModule(cfg Config, fifoPath string) (string, error) {
	args := []string{
		"load-module", "module-pipe-source",
		"source_name=" + cfg.Name,
		"file=" + fifoPath,
		"format=s16le",
		"rate=" + strconv.Itoa(cfg.Rate),
		"channels=" + strconv.Itoa(cfg.Channels),
		"source_properties=device.description=" + escapeDesc(cfg.Description),
	}
	out, err := exec.Command("pactl", args...).Output()
	if err != nil {
		return "", fmt.Errorf("pactl load-module: %w (%s)", err, stderr(err))
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", errors.New("pactl load-module returned empty module id")
	}
	return id, nil
}

func unloadModule(id string) error {
	if err := exec.Command("pactl", "unload-module", id).Run(); err != nil {
		return fmt.Errorf("pactl unload-module %s: %w", id, err)
	}
	return nil
}

// escapeDesc wraps the description in quotes if it contains spaces, so pactl
// parses it as a single property value.
func escapeDesc(d string) string {
	if d == "" {
		return "Virtual-Source"
	}
	if strings.ContainsAny(d, " \t") {
		return "\"" + d + "\""
	}
	return d
}

func stderr(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return strings.TrimSpace(string(ee.Stderr))
	}
	return ""
}
