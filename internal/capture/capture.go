// Package capture grabs audio from a Windows capture device (the VB-Cable
// output that Voicemod feeds) via WASAPI and emits framed wire packets.
//
// The real implementation lives in capture_windows.go (cgo + miniaudio/malgo).
// capture_other.go provides stubs so the package builds everywhere.
package capture

// Config tunes the capture device and framing.
type Config struct {
	DeviceSubstr string // capture device name substring to match
	Rate         int    // sample rate, Hz
	Channels     int    // channel count
	FrameMS      int    // capture period, ms (smaller = lower latency)
	QueueLen     int    // sender queue depth before dropping packets
}
