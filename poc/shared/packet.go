// Package shared defines the wire format used between the Windows transmitter
// and the Linux receiver.
//
// PoC design notes:
//   - The "network" is the Docker/KVM virtual bridge between the Winboat guest
//     and the Linux host, so it is effectively localhost: sub-ms, high
//     bandwidth, near-zero loss. We therefore send RAW PCM (no Opus/AAC) to
//     avoid codec encode/decode latency entirely.
//   - Audio is interleaved signed-16-bit little-endian (s16le), 48 kHz stereo
//     by default. 48k * 2ch * 2bytes = ~1.5 Mbps — trivial over a local bridge.
//   - Each datagram carries one capture period. A 32-bit sequence number lets
//     the receiver detect gaps and insert silence instead of desyncing.
package shared

import (
	"encoding/binary"
	"errors"
)

const (
	// Magic identifies our datagrams ("VM"). Cheap sanity check against
	// stray traffic on the UDP port.
	Magic = 0x564D // 'V','M'

	// Version of the wire format. Bump on any layout change.
	Version = 1

	// HeaderSize is the fixed byte length of the packet header.
	//   off 0..1  magic    uint16 BE
	//   off 2     version  uint8
	//   off 3     channels uint8
	//   off 4..7  seq      uint32 LE  (wraps; gap detection is modular)
	//   off 8..11 frames   uint32 LE  (sample-frames in payload, per channel)
	HeaderSize = 12
)

var (
	ErrShort   = errors.New("packet shorter than header")
	ErrMagic   = errors.New("bad magic")
	ErrVersion = errors.New("unsupported version")
)

// Header is the per-datagram metadata.
type Header struct {
	Channels uint8
	Seq      uint32
	Frames   uint32 // sample-frames per channel in the payload
}

// Encode writes the header into the first HeaderSize bytes of dst.
// dst must be at least HeaderSize long. Returns the number of bytes written.
func (h Header) Encode(dst []byte) int {
	binary.BigEndian.PutUint16(dst[0:2], Magic)
	dst[2] = Version
	dst[3] = h.Channels
	binary.LittleEndian.PutUint32(dst[4:8], h.Seq)
	binary.LittleEndian.PutUint32(dst[8:12], h.Frames)
	return HeaderSize
}

// Decode parses a header from the front of buf and returns the payload slice
// (a sub-slice of buf, not a copy).
func Decode(buf []byte) (Header, []byte, error) {
	if len(buf) < HeaderSize {
		return Header{}, nil, ErrShort
	}
	if binary.BigEndian.Uint16(buf[0:2]) != Magic {
		return Header{}, nil, ErrMagic
	}
	if buf[2] != Version {
		return Header{}, nil, ErrVersion
	}
	h := Header{
		Channels: buf[3],
		Seq:      binary.LittleEndian.Uint32(buf[4:8]),
		Frames:   binary.LittleEndian.Uint32(buf[8:12]),
	}
	return h, buf[HeaderSize:], nil
}

// BytesPerFrame returns the payload bytes for one sample-frame (all channels),
// assuming s16le samples.
func BytesPerFrame(channels int) int { return channels * 2 }
