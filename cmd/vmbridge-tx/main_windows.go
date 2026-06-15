//go:build windows

// Command vmbridge-tx runs inside the Windows (Winboat) guest. It captures the
// VB-Cable output Voicemod feeds and streams raw PCM to the Linux receiver.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os/signal"
	"syscall"

	"vmbridge/internal/capture"
)

func main() {
	var (
		addr     = flag.String("addr", "", "receiver address host:port (Linux host), e.g. 192.168.1.50:5000")
		device   = flag.String("device", "CABLE Output", "capture device name substring")
		rate     = flag.Int("rate", 48000, "sample rate Hz")
		channels = flag.Int("channels", 2, "channel count")
		frameMS  = flag.Int("frame", 5, "capture period in ms (smaller = lower latency)")
		queue    = flag.Int("queue", 8, "sender queue depth before dropping")
		list     = flag.Bool("list", false, "list capture devices and exit")
	)
	flag.Parse()

	if *list {
		names, err := capture.ListDevices()
		if err != nil {
			log.Fatalf("list devices: %v", err)
		}
		fmt.Println("Capture devices:")
		for i, n := range names {
			fmt.Printf("  [%d] %s\n", i, n)
		}
		return
	}
	if *addr == "" {
		log.Fatal("-addr is required (Linux host ip:port). Use -list to see devices.")
	}

	if name, err := capture.DeviceName(*device); err == nil {
		log.Printf("capturing device: %s", name)
	} else {
		log.Fatalf("%v", err)
	}

	conn, err := net.Dial("udp", *addr)
	if err != nil {
		log.Fatalf("dial udp %s: %v", *addr, err)
	}
	defer conn.Close()
	log.Printf("streaming to %s (%d Hz, %dch, %dms period)", *addr, *rate, *channels, *frameMS)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	send := func(pkt []byte) {
		if _, err := conn.Write(pkt); err != nil {
			log.Printf("udp write: %v", err)
		}
	}

	err = capture.Run(ctx, capture.Config{
		DeviceSubstr: *device,
		Rate:         *rate,
		Channels:     *channels,
		FrameMS:      *frameMS,
		QueueLen:     *queue,
	}, send)
	if err != nil {
		log.Printf("capture stopped: %v", err)
	}
	log.Println("stopped")
}
