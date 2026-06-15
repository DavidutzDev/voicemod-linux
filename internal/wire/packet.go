// Package wire defines the UDP datagram format shared by the transmitter
// (Windows) and receiver (Linux).
//
// The link between the Winboat guest and the Linux host is a local Docker/KVM
// bridge — effectively localhost: sub-ms, high bandwidth, near-zero loss. We
// therefore send RAW interleaved s16le PCM (no codec) to avoid encode/decode
// latency. Each datagram carries one capture period plus a 32-bit sequence
// number so the receiver can detect gaps and insert silence.
package wire

import (
	"encoding/binary"
	"errors"
)

const (
	// Magic identifies our datagrams ("VM"); cheap guard against stray UDP.
	Magic = 0x564D
	// Version of the layout; bump on any change.
	Version = 1
	// HeaderSize is the fixed header length in bytes:
	//   0..1  magic    uint16 BE
	//   2     version  uint8
	//   3     channels uint8
	//   4..7  seq      uint32 LE
	//   8..11 frames   uint32 LE  (sample-frames per channel in payload)
	HeaderSize = 12
)

var (
	ErrShort   = errors.New("wire: packet shorter than header")
	ErrMagic   = errors.New("wire: bad magic")
	ErrVersion = errors.New("wire: unsupported version")
)

// Header is the per-datagram metadata.
type Header struct {
	Channels uint8
	Seq      uint32
	Frames   uint32
}

// Encode writes the header into the first HeaderSize bytes of dst.
func (h Header) Encode(dst []byte) int {
	binary.BigEndian.PutUint16(dst[0:2], Magic)
	dst[2] = Version
	dst[3] = h.Channels
	binary.LittleEndian.PutUint32(dst[4:8], h.Seq)
	binary.LittleEndian.PutUint32(dst[8:12], h.Frames)
	return HeaderSize
}

// Decode parses a header from the front of buf and returns the payload
// sub-slice (not a copy).
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
	return Header{
		Channels: buf[3],
		Seq:      binary.LittleEndian.Uint32(buf[4:8]),
		Frames:   binary.LittleEndian.Uint32(buf[8:12]),
	}, buf[HeaderSize:], nil
}

// BytesPerFrame returns payload bytes for one sample-frame (all channels), s16le.
func BytesPerFrame(channels int) int { return channels * 2 }
