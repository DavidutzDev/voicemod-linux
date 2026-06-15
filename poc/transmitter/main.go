// Command transmitter runs inside the Windows (Winboat) guest.
//
// It captures the VB-Cable output device via WASAPI (event-driven shared mode,
// through miniaudio/malgo), frames the raw PCM with a sequence number, and
// fires each capture period to the Linux receiver over UDP.
//
// Signal path on Windows:
//
//	Voicemod  ->  VB-Cable Input  ->  "CABLE Output"  ->  [this program]  ->  UDP
//
// Build/run on Windows (cgo + a C toolchain such as MinGW required by malgo):
//
//	set CGO_ENABLED=1
//	go build -o transmitter.exe ./transmitter
//	transmitter.exe -addr 192.168.x.x:5000 -device "CABLE Output"
//
// HOT-PATH NOTE: the audio data callback must not block or allocate. We copy
// each period into a pooled buffer and hand it to a sender goroutine over a
// buffered channel; if the channel is full we DROP the packet rather than
// stall the real-time capture thread. The Go GC never runs inside miniaudio's
// device thread (that thread is owned by C), so the remaining risk is just the
// channel send, which is allocation-free here.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/gen2brain/malgo"

	"voicemod-bridge/shared"
)

func main() {
	var (
		addr      = flag.String("addr", "", "receiver address host:port (Linux host), e.g. 192.168.1.50:5000")
		deviceSub = flag.String("device", "CABLE Output", "capture device name substring to match")
		rate      = flag.Int("rate", 48000, "sample rate Hz")
		channels  = flag.Int("channels", 2, "channel count")
		frameMS   = flag.Int("frame", 5, "capture period in milliseconds (smaller = lower latency)")
		queueLen  = flag.Int("queue", 8, "sender queue depth in packets before dropping")
		list      = flag.Bool("list", false, "list capture devices and exit")
	)
	flag.Parse()

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(string) {})
	if err != nil {
		log.Fatalf("init malgo context: %v", err)
	}
	defer func() { _ = ctx.Uninit(); ctx.Free() }()

	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		log.Fatalf("enumerate capture devices: %v", err)
	}
	if *list {
		fmt.Println("Capture devices:")
		for i, d := range infos {
			fmt.Printf("  [%d] %s\n", i, d.Name())
		}
		return
	}
	if *addr == "" {
		log.Fatal("-addr is required (Linux host ip:port). Use -list to see devices.")
	}

	// Pick the capture device whose name contains -device (case-insensitive).
	picked := -1
	for i, d := range infos {
		if strings.Contains(strings.ToLower(d.Name()), strings.ToLower(*deviceSub)) {
			picked = i
			break
		}
	}
	if picked < 0 {
		log.Printf("no capture device matching %q. Available:", *deviceSub)
		for i, d := range infos {
			log.Printf("  [%d] %s", i, d.Name())
		}
		log.Fatal("aborting")
	}
	log.Printf("capturing device: %s", infos[picked].Name())

	conn, err := net.Dial("udp", *addr)
	if err != nil {
		log.Fatalf("dial udp %s: %v", *addr, err)
	}
	defer conn.Close()
	log.Printf("streaming to %s", *addr)

	framesPerPeriod := *rate * *frameMS / 1000
	payloadBytes := framesPerPeriod * shared.BytesPerFrame(*channels)
	pktBytes := shared.HeaderSize + payloadBytes

	// Pool of full-packet buffers, reused to keep the hot path allocation-free.
	pool := &sync.Pool{New: func() any { b := make([]byte, pktBytes); return &b }}

	// Buffered channel decouples the RT capture thread from the UDP sender.
	queue := make(chan *[]byte, *queueLen)
	var seq uint32
	var dropped uint64

	// Sender goroutine: drains the queue and writes to the socket.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for buf := range queue {
			if _, err := conn.Write(*buf); err != nil {
				log.Printf("udp write: %v", err)
			}
			pool.Put(buf)
		}
	}()

	onData := func(_, in []byte, frames uint32) {
		// in: interleaved s16le capture data for this period.
		bufp := pool.Get().(*[]byte)
		buf := *bufp
		need := shared.HeaderSize + len(in)
		if cap(buf) < need {
			buf = make([]byte, need)
		}
		buf = buf[:need]
		h := shared.Header{
			Channels: uint8(*channels),
			Seq:      atomic.AddUint32(&seq, 1) - 1,
			Frames:   frames,
		}
		h.Encode(buf)
		copy(buf[shared.HeaderSize:], in)
		*bufp = buf

		select {
		case queue <- bufp:
		default:
			// Queue full — drop rather than block the audio thread.
			pool.Put(bufp)
			atomic.AddUint64(&dropped, 1)
		}
	}

	devCfg := malgo.DefaultDeviceConfig(malgo.Capture)
	devCfg.Capture.Format = malgo.FormatS16
	devCfg.Capture.Channels = uint32(*channels)
	devCfg.Capture.DeviceID = infos[picked].ID.Pointer()
	devCfg.SampleRate = uint32(*rate)
	devCfg.PeriodSizeInFrames = uint32(framesPerPeriod)
	devCfg.Periods = 2
	// WASAPI low-latency / shared event-driven mode.
	devCfg.Wasapi.NoAutoConvertSRC = 1

	device, err := malgo.InitDevice(ctx.Context, devCfg, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		log.Fatalf("init capture device: %v", err)
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		log.Fatalf("start capture: %v", err)
	}
	log.Printf("capturing: %d Hz, %dch, %dms period (%d frames, %d B/pkt). Ctrl-C to stop.",
		*rate, *channels, *frameMS, framesPerPeriod, pktBytes)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	_ = device.Stop()
	close(queue)
	wg.Wait()
	if d := atomic.LoadUint64(&dropped); d > 0 {
		log.Printf("dropped %d packets (sender queue full)", d)
	}
	log.Println("stopped")
}
