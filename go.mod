module vmbridge

go 1.26

// malgo (WASAPI capture) is only compiled into the Windows transmitter.
// The Linux receiver builds with pure stdlib — no cgo.
require github.com/gen2brain/malgo v0.11.23
