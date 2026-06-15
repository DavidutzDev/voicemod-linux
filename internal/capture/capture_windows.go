//go:build windows

package capture

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gen2brain/malgo"

	"vmbridge/internal/wire"
)

// ListDevices returns the names of available capture devices.
func ListDevices() ([]string, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(string) {})
	if err != nil {
		return nil, fmt.Errorf("init malgo context: %w", err)
	}
	defer func() { _ = ctx.Uninit(); ctx.Free() }()

	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(infos))
	for i, d := range infos {
		names[i] = d.Name()
	}
	return names, nil
}

// Run captures from the matching device and calls send for each framed wire
// packet until ctx is cancelled. send must not retain the slice past the call;
// it is reused from a pool. The audio callback never blocks: if the consumer
// is slow, packets are dropped (count returned via the dropped counter logged
// at shutdown) rather than stalling the real-time device thread.
func Run(ctx context.Context, cfg Config, send func([]byte)) error {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(string) {})
	if err != nil {
		return fmt.Errorf("init malgo context: %w", err)
	}
	defer func() { _ = mctx.Uninit(); mctx.Free() }()

	infos, err := mctx.Devices(malgo.Capture)
	if err != nil {
		return err
	}
	picked := -1
	for i, d := range infos {
		if strings.Contains(strings.ToLower(d.Name()), strings.ToLower(cfg.DeviceSubstr)) {
			picked = i
			break
		}
	}
	if picked < 0 {
		return fmt.Errorf("no capture device matching %q", cfg.DeviceSubstr)
	}

	framesPerPeriod := cfg.Rate * cfg.FrameMS / 1000
	pktBytes := wire.HeaderSize + framesPerPeriod*wire.BytesPerFrame(cfg.Channels)

	pool := &sync.Pool{New: func() any { b := make([]byte, pktBytes); return &b }}
	queue := make(chan *[]byte, max(cfg.QueueLen, 1))
	var seq uint32
	var dropped uint64

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for bufp := range queue {
			send(*bufp)
			pool.Put(bufp)
		}
	}()

	onData := func(_, in []byte, frames uint32) {
		bufp := pool.Get().(*[]byte)
		buf := *bufp
		need := wire.HeaderSize + len(in)
		if cap(buf) < need {
			buf = make([]byte, need)
		}
		buf = buf[:need]
		h := wire.Header{
			Channels: uint8(cfg.Channels),
			Seq:      atomic.AddUint32(&seq, 1) - 1,
			Frames:   frames,
		}
		h.Encode(buf)
		copy(buf[wire.HeaderSize:], in)
		*bufp = buf

		select {
		case queue <- bufp:
		default:
			pool.Put(bufp)
			atomic.AddUint64(&dropped, 1)
		}
	}

	devCfg := malgo.DefaultDeviceConfig(malgo.Capture)
	devCfg.Capture.Format = malgo.FormatS16
	devCfg.Capture.Channels = uint32(cfg.Channels)
	devCfg.Capture.DeviceID = infos[picked].ID.Pointer()
	devCfg.SampleRate = uint32(cfg.Rate)
	devCfg.PeriodSizeInFrames = uint32(framesPerPeriod)
	devCfg.Periods = 2
	devCfg.Wasapi.NoAutoConvertSRC = 1

	device, err := malgo.InitDevice(mctx.Context, devCfg, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		return fmt.Errorf("init capture device: %w", err)
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		return fmt.Errorf("start capture: %w", err)
	}

	<-ctx.Done()
	_ = device.Stop()
	close(queue)
	wg.Wait()
	if d := atomic.LoadUint64(&dropped); d > 0 {
		return fmt.Errorf("capture stopped; dropped %d packets (consumer too slow)", d)
	}
	return nil
}

// DeviceName returns the picked device's display name for the given substring,
// or an error if none match. Convenience for logging.
func DeviceName(substr string) (string, error) {
	names, err := ListDevices()
	if err != nil {
		return "", err
	}
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), strings.ToLower(substr)) {
			return n, nil
		}
	}
	return "", fmt.Errorf("no capture device matching %q", substr)
}
