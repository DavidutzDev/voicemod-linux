module voicemod-bridge

go 1.26

// Transmitter (Windows) needs malgo for WASAPI capture (cgo).
// Receiver (Linux) is pure stdlib — `go build ./receiver` needs no cgo.
require github.com/gen2brain/malgo v0.11.23
