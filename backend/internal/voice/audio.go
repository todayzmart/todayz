package voice

import (
	"bytes"
	"encoding/binary"
	"math"
	"time"
)

// The server's side of the MaxSeconds limit (01 §17): the app stops recording there, and the
// upload is refused past it. The length comes from the container where it says (wav, webm,
// ogg, mp4); otherwise the recording may be no bigger than MaxBitrate of its type for
// MaxSeconds.

// DurationGrace is how far past MaxSeconds a measured recording may run: the browser stops
// the recorder a little after the app's timer fires.
const DurationGrace = 500 * time.Millisecond

// MaxBitrate is the highest bit rate, per media type, a recording of the app may have: Opus
// and AAC as browsers record speech, MP3 at its top rate, 16-bit mono PCM at 16 kHz. It
// bounds a recording whose length the container does not say.
var MaxBitrate = map[string]int{
	"audio/webm": 128_000,
	"audio/ogg":  128_000,
	"audio/mp4":  128_000,
	"audio/mpeg": 320_000,
	"audio/wav":  256_000,
}

// maxBytes is the size bound for a recording of mediaType whose length is not known.
func maxBytes(mediaType string) int {
	return MaxBitrate[mediaType] / 8 * MaxSeconds
}

// tooLong reports whether rec runs past MaxSeconds, and the length the speech cost is
// charged on: the container's (measured), or MaxSeconds when it does not say.
func tooLong(rec Recording) (long bool, length time.Duration, measured bool) {
	limit := MaxSeconds * time.Second
	if d, ok := audioDuration(rec.Data, rec.MediaType); ok {
		return d > limit+DurationGrace, d, true
	}
	return len(rec.Data) > maxBytes(rec.MediaType), limit, false
}

// audioDuration reads the length a container states. ok is false when it does not say or
// the bytes are not that container.
func audioDuration(b []byte, mediaType string) (time.Duration, bool) {
	switch mediaType {
	case "audio/wav":
		return wavDuration(b)
	case "audio/webm":
		return webmDuration(b)
	case "audio/ogg":
		return oggDuration(b)
	case "audio/mp4":
		return mp4Duration(b)
	}
	return 0, false
}

// within is n as an int when it fits in room (a non-negative length): every size a container
// states meets a slice index here, so a hostile 64-bit size cannot wrap.
func within(n uint64, room int) (int, bool) {
	if room < 0 || n > uint64(room) {
		return 0, false
	}
	return int(n), true // #nosec G115 -- n ≤ room, which is an int
}

func seconds(f float64) (time.Duration, bool) {
	if math.IsNaN(f) || f < 0 || f > 24*3600 {
		return 0, false
	}
	return time.Duration(f * float64(time.Second)), true
}

// wavDuration: the data chunk's size over the fmt chunk's byte rate.
func wavDuration(b []byte) (time.Duration, bool) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return 0, false
	}
	var byteRate uint32
	for p := 12; p+8 <= len(b); {
		id, size := string(b[p:p+4]), binary.LittleEndian.Uint32(b[p+4:p+8])
		body := p + 8
		switch id {
		case "fmt ":
			if body+12 > len(b) {
				return 0, false
			}
			byteRate = binary.LittleEndian.Uint32(b[body+8 : body+12])
		case "data":
			if byteRate == 0 {
				return 0, false
			}
			// A streaming writer may leave the size at 0 or past the end: count what is here.
			n, ok := within(uint64(size), len(b)-body)
			if size == 0 || !ok {
				n = len(b) - body
			}
			return seconds(float64(n) / float64(byteRate))
		}
		step, ok := within(uint64(size)+uint64(size&1), len(b)-body)
		if !ok {
			return 0, false
		}
		p = body + step
	}
	return 0, false
}

// Matroska element ids webmDuration reads.
const (
	ebmlHeader    = 0x1A45DFA3
	mkvSegment    = 0x18538067
	mkvInfo       = 0x1549A966
	mkvScale      = 0x2AD7B1
	mkvDuration   = 0x4489
	mkvCluster    = 0x1F43B675
	mkvTimecode   = 0xE7
	mkvBlockGroup = 0xA0
	mkvBlock      = 0xA1
	mkvSimple     = 0xA3
)

// webmDuration reads Info's Duration, or, as browsers' recorders leave it out, the last
// block's time. The masters it needs are entered rather than skipped, so a segment or
// cluster of unknown size (a live recording) reads the same. A file it cannot walk to the
// end says nothing (its last block may be further on).
func webmDuration(b []byte) (time.Duration, bool) {
	if len(b) < 4 || binary.BigEndian.Uint32(b) != ebmlHeader {
		return 0, false
	}
	scale := uint64(1_000_000) // TimecodeScale default: 1 ms
	var stated float64
	var cluster, last int64
	blocks := false
	p := 0
	for p < len(b) {
		id, n := ebmlID(b[p:])
		if n == 0 {
			break
		}
		size, m, unknown := ebmlSize(b[p+n:])
		if m == 0 {
			break
		}
		body := p + n + m
		switch id {
		case mkvSegment, mkvInfo, mkvCluster, mkvBlockGroup:
			p = body
			continue
		}
		n, ok := within(size, len(b)-body)
		if unknown || !ok {
			break
		}
		v := b[body : body+n]
		switch id {
		case mkvScale:
			if u := beUint(v); u > 0 {
				scale = u
			}
		case mkvDuration:
			switch len(v) {
			case 4:
				stated = float64(math.Float32frombits(binary.BigEndian.Uint32(v)))
			case 8:
				stated = math.Float64frombits(binary.BigEndian.Uint64(v))
			}
		case mkvTimecode:
			cluster = int64(beUint(v)) // #nosec G115 -- a recording's timecode is far below 2^63
		case mkvSimple, mkvBlock:
			// track number (a vint), then a signed 16-bit time relative to the cluster
			if _, k, _ := ebmlSize(v); k > 0 && len(v) >= k+2 {
				t := cluster + int64(int16(binary.BigEndian.Uint16(v[k:k+2]))) // #nosec G115 -- the field is a signed 16-bit value
				last, blocks = max(last, t), true
			}
		}
		p = body + n
	}
	if p < len(b) {
		return 0, false
	}
	unit := float64(scale) / 1e9
	if stated > 0 {
		return seconds(stated * unit)
	}
	if blocks {
		return seconds(float64(last) * unit)
	}
	return 0, false
}

// ebmlID reads an element id (its length marker kept, as the spec writes ids).
func ebmlID(b []byte) (uint32, int) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0
	}
	n := 1
	for mask := byte(0x80); b[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 4 || len(b) < n {
		return 0, 0
	}
	var id uint32
	for _, c := range b[:n] {
		id = id<<8 | uint32(c)
	}
	return id, n
}

// ebmlSize reads a size vint; unknown is the all-ones value (size not known yet).
func ebmlSize(b []byte) (size uint64, n int, unknown bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false
	}
	n = 1
	mask := byte(0x80)
	for ; b[0]&mask == 0; mask >>= 1 {
		n++
	}
	if len(b) < n {
		return 0, 0, false
	}
	size = uint64(b[0] & (mask - 1))
	ones := size == uint64(mask-1)
	for _, c := range b[1:n] {
		size = size<<8 | uint64(c)
		ones = ones && c == 0xFF
	}
	return size, n, ones
}

func beUint(v []byte) uint64 {
	if len(v) > 8 {
		return 0
	}
	var u uint64
	for _, c := range v {
		u = u<<8 | uint64(c)
	}
	return u
}

// oggDuration: the last page's granule position over the sample rate (Opus: always 48 kHz,
// less the pre-skip; Vorbis: the rate in its header).
func oggDuration(b []byte) (time.Duration, bool) {
	if len(b) < 28 || string(b[0:4]) != "OggS" {
		return 0, false
	}
	first := b[27+int(b[26]):]
	var rate, skip float64
	switch {
	case bytes.HasPrefix(first, []byte("OpusHead")) && len(first) >= 12:
		rate, skip = 48_000, float64(binary.LittleEndian.Uint16(first[10:12]))
	case bytes.HasPrefix(first, []byte("\x01vorbis")) && len(first) >= 16:
		rate = float64(binary.LittleEndian.Uint32(first[12:16]))
	default:
		return 0, false
	}
	if rate == 0 {
		return 0, false
	}
	// The page header: "OggS", version, flags, then the 8-byte granule position.
	at := bytes.LastIndex(b, []byte("OggS"))
	granule := at + len("OggS") + 2
	if at < 0 || granule+8 > len(b) {
		return 0, false
	}
	g := binary.LittleEndian.Uint64(b[granule : granule+8])
	if g == math.MaxUint64 { // a page where no packet ends
		return 0, false
	}
	return seconds(math.Max(0, float64(g)-skip) / rate)
}

// mp4Duration: moov/mvhd's duration over its time scale. A fragmented file (Safari's
// recorder) states 0 there, and its length is not known.
func mp4Duration(b []byte) (time.Duration, bool) {
	moov, ok := mp4Box(b, "moov")
	if !ok {
		return 0, false
	}
	mvhd, ok := mp4Box(moov, "mvhd")
	if !ok || len(mvhd) < 4 {
		return 0, false
	}
	var scale, dur uint64
	switch mvhd[0] {
	case 0:
		if len(mvhd) < 20 {
			return 0, false
		}
		scale, dur = uint64(binary.BigEndian.Uint32(mvhd[12:16])), uint64(binary.BigEndian.Uint32(mvhd[16:20]))
	case 1:
		if len(mvhd) < 32 {
			return 0, false
		}
		scale, dur = uint64(binary.BigEndian.Uint32(mvhd[20:24])), binary.BigEndian.Uint64(mvhd[24:32])
	default:
		return 0, false
	}
	if scale == 0 || dur == 0 || dur == math.MaxUint32 || dur == math.MaxUint64 {
		return 0, false
	}
	return seconds(float64(dur) / float64(scale))
}

// mp4Box finds the first box of type t in b and returns its body.
func mp4Box(b []byte, t string) ([]byte, bool) {
	for p := 0; p+8 <= len(b); {
		size, head := uint64(binary.BigEndian.Uint32(b[p:p+4])), 8
		n, ok := within(size, len(b)-p)
		switch size {
		case 0: // to the end of the file
			n, ok = len(b)-p, true
		case 1: // a 64-bit size follows the type
			if p+16 > len(b) {
				return nil, false
			}
			head = 16
			n, ok = within(binary.BigEndian.Uint64(b[p+8:p+16]), len(b)-p)
		}
		if !ok || n < head {
			return nil, false
		}
		if string(b[p+4:p+8]) == t {
			return b[p+head : p+n], true
		}
		p += n
	}
	return nil, false
}
