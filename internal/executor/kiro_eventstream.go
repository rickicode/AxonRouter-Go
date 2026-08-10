package executor

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
)

// EventFrame represents a parsed AWS EventStream message (used by Kiro / CodeWhisperer).
type EventFrame struct {
	Headers map[string]string
	Payload json.RawMessage
}

// byteQueue is a growable contiguous byte buffer with cheap peek/read support.
// The underlying slice is reused and compacted when the consumed prefix grows
// large, avoiding the slice-of-slices allocation pattern.
type byteQueue struct {
	buf    []byte
	head   int
	length int
}

func newByteQueue() *byteQueue { return &byteQueue{} }

func (q *byteQueue) push(data []byte) {
	if len(data) == 0 {
		return
	}
	q.buf = append(q.buf, data...)
	q.length += len(data)
}

func (q *byteQueue) len() int { return q.length }

func (q *byteQueue) byteAt(offset int) byte {
	if offset < 0 || offset >= q.length {
		return 0
	}
	return q.buf[q.head+offset]
}

func (q *byteQueue) peekUint32BE(offset int) (uint32, bool) {
	if q.length < offset+4 {
		return 0, false
	}
	start := q.head + offset
	return binary.BigEndian.Uint32(q.buf[start : start+4]), true
}

func (q *byteQueue) read(n int) []byte {
	if n <= 0 || q.length < n {
		return nil
	}
	start := q.head
	end := start + n
	out := append([]byte(nil), q.buf[start:end]...)
	q.head = end
	q.length -= n

	// Compact when the consumed prefix is larger than the remaining data so the
	// buffer does not grow indefinitely on a long-lived stream.
	if q.head > q.length && q.length > 0 {
		copy(q.buf, q.buf[q.head:q.head+q.length])
		q.head = 0
		q.buf = q.buf[:q.length]
	} else if q.length == 0 {
		q.head = 0
		q.buf = q.buf[:0]
	}
	return out
}

var crc32Table = crc32.MakeTable(crc32.IEEE)

func crc32IEEE(data []byte) uint32 {
	return crc32.Checksum(data, crc32Table)
}

// parseEventFrame parses an AWS EventStream frame.
// See OmniRoute open-sse/executors/kiro/eventstream.ts.
func parseEventFrame(data []byte) (*EventFrame, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("frame too short: %d bytes", len(data))
	}
	totalLength := binary.BigEndian.Uint32(data[0:4])
	headersLength := binary.BigEndian.Uint32(data[4:8])
	preludeCRC := binary.BigEndian.Uint32(data[8:12])

	if int(totalLength) != len(data) {
		return nil, fmt.Errorf("frame length mismatch: total=%d got=%d", totalLength, len(data))
	}

	if crc32IEEE(data[0:8]) != preludeCRC {
		return nil, fmt.Errorf("prelude CRC mismatch")
	}

	headers := make(map[string]string)
	offset := 12
	headerEnd := 12 + int(headersLength)
	if headerEnd > len(data)-4 {
		return nil, fmt.Errorf("invalid headers length")
	}
	for offset < headerEnd {
		if offset >= len(data) {
			break
		}
		nameLen := int(data[offset])
		offset++
		if offset+nameLen > len(data) {
			break
		}
		name := string(data[offset : offset+nameLen])
		offset += nameLen
		if offset >= len(data) {
			break
		}
		headerType := data[offset]
		offset++
		if headerType == 7 { // string
			if offset+2 > len(data) {
				break
			}
			valueLen := binary.BigEndian.Uint16(data[offset : offset+2])
			offset += 2
			if offset+int(valueLen) > len(data) {
				break
			}
			value := string(data[offset : offset+int(valueLen)])
			offset += int(valueLen)
			headers[name] = value
		} else {
			// unsupported header type: stop parsing
			break
		}
	}

	payloadStart := 12 + int(headersLength)
	payloadEnd := len(data) - 4
	var payload json.RawMessage
	if payloadEnd > payloadStart {
		s := bytes.TrimSpace(data[payloadStart:payloadEnd])
		if len(s) > 0 {
			payload = append([]byte(nil), s...)
		}
	}
	return &EventFrame{Headers: headers, Payload: payload}, nil
}
