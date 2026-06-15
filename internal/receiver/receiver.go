// Package receiver implements the host-side ingest loop: read PCM datagrams,
// fill gaps from lost/late packets with silence so the stream never desyncs,
// and write the raw PCM to a sink (the virtual source).
package receiver

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync/atomic"
	"time"

	"vmbridge/internal/wire"
)

// Config tunes the ingest loop.
type Config struct {
	Listen     string        // UDP listen address, e.g. ":5000"
	MaxFill    int           // cap on silence frames inserted per gap
	ReadBuf    int           // UDP read buffer size in bytes
	StatsEvery time.Duration // 0 disables periodic stats logging
}

// Stats are cumulative counters, safe to read concurrently.
type Stats struct {
	Packets, Bytes, Lost, Late, Bad atomic.Uint64
}

// Run blocks reading datagrams and writing PCM to sink until ctx is cancelled
// or a fatal error occurs. Returns nil on clean (context-cancelled) shutdown.
func Run(ctx context.Context, cfg Config, sink io.Writer, st *Stats) error {
	if cfg.ReadBuf <= 0 {
		cfg.ReadBuf = 64 * 1024
	}
	if cfg.MaxFill <= 0 {
		cfg.MaxFill = 50
	}
	if st == nil {
		st = &Stats{}
	}

	pc, err := net.ListenPacket("udp", cfg.Listen)
	if err != nil {
		return err
	}
	defer pc.Close()
	log.Printf("receiver listening on %s", cfg.Listen)

	// Cancellation: closing the socket unblocks ReadFrom.
	go func() {
		<-ctx.Done()
		_ = pc.Close()
	}()

	if cfg.StatsEvery > 0 {
		go reportStats(ctx, cfg.StatsEvery, st)
	}

	rbuf := make([]byte, cfg.ReadBuf)
	silence := make([]byte, 4096)
	var expect uint32
	var have bool

	for {
		n, _, err := pc.ReadFrom(rbuf)
		if err != nil {
			if ctx.Err() != nil {
				return nil // clean shutdown
			}
			log.Printf("udp read: %v", err)
			continue
		}
		h, payload, err := wire.Decode(rbuf[:n])
		if err != nil {
			st.Bad.Add(1)
			continue
		}

		if !have {
			expect, have = h.Seq, true
		}

		switch {
		case h.Seq == expect:
			// in order
		case int32(h.Seq-expect) > 0:
			missing := h.Seq - expect
			fill := int(missing)
			if fill > cfg.MaxFill {
				fill = cfg.MaxFill
			}
			st.Lost.Add(uint64(missing))
			writeSilence(sink, silence, fill*len(payload))
		default:
			st.Late.Add(1)
			continue // late/dup; keep expect
		}
		expect = h.Seq + 1

		if _, err := sink.Write(payload); err != nil {
			return err
		}
		st.Packets.Add(1)
		st.Bytes.Add(uint64(len(payload)))
	}
}

func writeSilence(w io.Writer, zero []byte, n int) {
	for n > 0 {
		chunk := min(n, len(zero))
		if _, err := w.Write(zero[:chunk]); err != nil {
			return
		}
		n -= chunk
	}
}

func reportStats(ctx context.Context, every time.Duration, st *Stats) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if errors.Is(ctx.Err(), context.Canceled) {
				return
			}
			log.Printf("rx pkts=%d bytes=%d lost=%d late=%d bad=%d",
				st.Packets.Load(), st.Bytes.Load(), st.Lost.Load(),
				st.Late.Load(), st.Bad.Load())
		}
	}
}
