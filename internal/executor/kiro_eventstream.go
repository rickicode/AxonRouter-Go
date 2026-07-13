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
	Payload map[string]any
}

// byteQueue is a simple growable byte buffer with peek/read support.
type byteQueue struct {
	chunks [][]byte
	head   int
	length int
}

func newByteQueue() *byteQueue { return &byteQueue{} }

func (q *byteQueue) push(data []byte) {
	if len(data) == 0 {
		return
	}
	q.chunks = append(q.chunks, data)
	q.length += len(data)
}

func (q *byteQueue) len() int { return q.length }

func (q *byteQueue) byteAt(offset int) byte {
	remaining := offset
	for i, chunk := range q.chunks {
		start := 0
		if i == 0 {
			start = q.head
		}
		available := len(chunk) - start
		if remaining < available {
			return chunk[start+remaining]
		}
		remaining -= available
	}
	return 0
}

func (q *byteQueue) peekUint32BE(offset int) (uint32, bool) {
	if q.length < offset+4 {
		return 0, false
	}
	return binary.BigEndian.Uint32([]byte{
		q.byteAt(offset),
		q.byteAt(offset + 1),
		q.byteAt(offset + 2),
		q.byteAt(offset + 3),
	}), true
}

func (q *byteQueue) read(n int) []byte {
	if n <= 0 || q.length < n {
		return nil
	}
	out := make([]byte, n)
	written := 0
	for written < n {
		headChunk := q.chunks[0]
		available := len(headChunk) - q.head
		take := available
		if n-written < take {
			take = n - written
		}
		copy(out[written:], headChunk[q.head:q.head+take])
		q.head += take
		q.length -= take
		written += take
		if q.head >= len(headChunk) {
			q.chunks = q.chunks[1:]
			q.head = 0
		}
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
	var payload map[string]any
	if payloadEnd > payloadStart {
		s := bytes.TrimSpace(data[payloadStart:payloadEnd])
		if len(s) > 0 {
			if err := json.Unmarshal(s, &payload); err != nil {
				return nil, fmt.Errorf("payload JSON parse: %w", err)
			}
		}
	}
	return &EventFrame{Headers: headers, Payload: payload}, nil
}
