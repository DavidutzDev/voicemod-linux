//go:build linux

// Command vmbridge-rx runs on the Linux host. It registers a virtual
// microphone, receives PCM over UDP from the Windows transmitter, and writes it
// into that microphone — then unregisters the microphone automatically on exit.
// No manual pactl scripts required.
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	"vmbridge/internal/receiver"
	"vmbridge/internal/source"
)

func main() {
	var (
		listen   = flag.String("listen", ":5000", "UDP listen address")
		name     = flag.String("name", "voicemod", "virtual source name")
		desc     = flag.String("desc", "Voicemod", "device description shown in app pickers")
		rate     = flag.Int("rate", 48000, "sample rate Hz")
		channels = flag.Int("channels", 2, "channel count")
		fifo     = flag.String("fifo", "", "backing FIFO path (default /tmp/<name>.fifo)")
		maxFill  = flag.Int("maxfill", 50, "max silence frames inserted per gap")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("registering virtual source %q (%d Hz, %dch)...", *name, *rate, *channels)
	sink, err := source.Register(source.Config{
		Name:        *name,
		Description: *desc,
		Rate:        *rate,
		Channels:    *channels,
		FifoPath:    *fifo,
	})
	if err != nil {
		log.Fatalf("register source: %v", err)
	}
	// Guaranteed unregister on any normal exit or signal.
	defer func() {
		if err := sink.Close(); err != nil {
			log.Printf("unregister source: %v", err)
		} else {
			log.Printf("virtual source %q unregistered", *name)
		}
	}()
	log.Printf("virtual source %q ready — select %q as your mic", *name, *desc)

	var stats receiver.Stats
	err = receiver.Run(ctx, receiver.Config{
		Listen:     *listen,
		MaxFill:    *maxFill,
		StatsEvery: 5 * time.Second,
	}, sink, &stats)
	if err != nil {
		log.Printf("receiver stopped: %v", err)
	}
}
