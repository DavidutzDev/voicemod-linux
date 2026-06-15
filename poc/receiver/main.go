// Command receiver runs on the Linux host.
//
// It listens for the PCM datagrams sent by the Windows transmitter, fills gaps
// (lost/late packets) with silence so the stream never desyncs, and writes the
// raw s16le PCM into a FIFO. A PipeWire/PulseAudio `module-pipe-source` reads
// that FIFO and exposes it to every Linux app as a selectable microphone named
// "voicemod" (see scripts/setup-linux.sh).
//
// Signal path on Linux:
//
//	UDP  ->  [this program]  ->  /tmp/voicemod.fifo  ->  module-pipe-source  ->  apps
//
// Pure stdlib — no cgo. Build/run:
//
//	go build -o receiver ./receiver
//	./receiver -listen :5000 -fifo /tmp/voicemod.fifo
//
// PoC limitations (left for the "tier two" hardening pass):
//   - No adaptive resampling for clock drift. The Windows capture clock and the
//     PipeWire clock are independent crystals; over many minutes the small rate
//     mismatch will accumulate into an under/overrun. For a short PoC session
//     it is fine; for hours you must nudge a resampler off buffer fill level.
//   - The jitter buffer is minimal (in-order, drop-late). Good enough on a
//     local virtual bridge where reordering is rare.
package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"
	"sync/atomic"
	"time"

	"voicemod-bridge/shared"
)

func main() {
	var (
		listen   = flag.String("listen", ":5000", "UDP listen address")
		fifoPath = flag.String("fifo", "/tmp/voicemod.fifo", "output FIFO path (read by module-pipe-source)")
		maxFill  = flag.Int("maxfill", 50, "max silence frames to insert for one gap (caps runaway fill after a stall)")
		bufKB    = flag.Int("buf", 64, "UDP read buffer size in KiB")
	)
	flag.Parse()

	pc, err := net.ListenPacket("udp", *listen)
	if err != nil {
		log.Fatalf("listen %s: %v", *listen, err)
	}
	defer pc.Close()
	log.Printf("listening on %s", *listen)

	// Open the FIFO for writing. This BLOCKS until module-pipe-source opens the
	// read end, so run scripts/setup-linux.sh first.
	log.Printf("opening FIFO %s (blocks until the virtual source is loaded)...", *fifoPath)
	fifo, err := openFifo(*fifoPath)
	if err != nil {
		log.Fatalf("open fifo: %v", err)
	}
	log.Printf("FIFO open — virtual mic connected")

	rbuf := make([]byte, *bufKB*1024)
	silence := make([]byte, 4096) // reused zero buffer for gap fill

	var (
		expect     uint32
		haveStream bool
		stats      stat
	)
	go stats.report()

	for {
		n, _, err := pc.ReadFrom(rbuf)
		if err != nil {
			log.Printf("udp read: %v", err)
			continue
		}
		h, payload, err := shared.Decode(rbuf[:n])
		if err != nil {
			atomic.AddUint64(&stats.bad, 1)
			continue
		}

		if !haveStream {
			expect = h.Seq
			haveStream = true
		}

		switch {
		case h.Seq == expect:
			// in order
		case int32(h.Seq-expect) > 0:
			// gap: insert silence for the missing periods
			missing := h.Seq - expect
			fill := int(missing)
			if fill > *maxFill {
				fill = *maxFill
			}
			gapBytes := fill * len(payload)
			atomic.AddUint64(&stats.lost, uint64(missing))
			writeSilence(fifo, silence, gapBytes)
		default:
			// late or duplicate — drop, keep expect unchanged
			atomic.AddUint64(&stats.late, 1)
			continue
		}
		expect = h.Seq + 1

		if _, err := fifo.Write(payload); err != nil {
			// Reader (module-pipe-source) went away. Try to reopen.
			log.Printf("fifo write: %v — reopening", err)
			_ = fifo.Close()
			fifo, err = openFifo(*fifoPath)
			if err != nil {
				log.Fatalf("reopen fifo: %v", err)
			}
			haveStream = false
			continue
		}
		atomic.AddUint64(&stats.pkts, 1)
		atomic.AddUint64(&stats.bytes, uint64(len(payload)))
	}
}

func openFifo(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY, os.ModeNamedPipe)
}

func writeSilence(w io.Writer, zero []byte, n int) {
	for n > 0 {
		chunk := n
		if chunk > len(zero) {
			chunk = len(zero)
		}
		if _, err := w.Write(zero[:chunk]); err != nil {
			return
		}
		n -= chunk
	}
}

type stat struct {
	pkts  uint64
	bytes uint64
	lost  uint64
	late  uint64
	bad   uint64
}

func (s *stat) report() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for range t.C {
		log.Printf("rx pkts=%d bytes=%d lost=%d late=%d bad=%d",
			atomic.LoadUint64(&s.pkts), atomic.LoadUint64(&s.bytes),
			atomic.LoadUint64(&s.lost), atomic.LoadUint64(&s.late),
			atomic.LoadUint64(&s.bad))
	}
}
