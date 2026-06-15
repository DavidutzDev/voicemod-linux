// Package source manages a virtual capture device on the Linux host that
// applications (Discord, OBS, browsers) can select as a microphone.
//
// The lifecycle is owned entirely by this process: Register loads a
// PulseAudio/PipeWire module-pipe-source, and the returned Sink's Close unloads
// it again. No manual scripts, no leftover modules after exit.
package source

import "io"

// Config describes the virtual source to create.
type Config struct {
	Name        string // PulseAudio source_name, e.g. "voicemod"
	Description string // human label shown in app device pickers
	Rate        int    // sample rate, Hz
	Channels    int    // channel count
	FifoPath    string // backing FIFO; empty => /tmp/<Name>.fifo
}

func (c Config) fifo() string {
	if c.FifoPath != "" {
		return c.FifoPath
	}
	return "/tmp/" + c.Name + ".fifo"
}

// Sink is a registered virtual source. Write feeds it interleaved s16le PCM;
// Close unregisters it and cleans up.
type Sink interface {
	io.WriteCloser
}
