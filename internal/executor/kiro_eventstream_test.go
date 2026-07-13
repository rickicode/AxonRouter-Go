package executor

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func buildEventFrame(headers map[string]string, payload map[string]any) []byte {
	hdrLen := 0
	for k, v := range headers {
		hdrLen += 1 + len(k) + 1 + 2 + len(v)
	}

	var payloadBytes []byte
	if payload != nil {
		payloadBytes, _ = json.Marshal(payload)
	}

	totalLen := 12 + hdrLen + len(payloadBytes) + 4
	buf := make([]byte, totalLen)

	binary.BigEndian.PutUint32(buf[0:4], uint32(totalLen))
	binary.BigEndian.PutUint32(buf[4:8], uint32(hdrLen))
	binary.BigEndian.PutUint32(buf[8:12], crc32IEEE(buf[0:8]))

	offset := 12
	for k, v := range headers {
		buf[offset] = byte(len(k))
		offset++
		copy(buf[offset:], k)
		offset += len(k)
		buf[offset] = 7 // string type
		offset++
		binary.BigEndian.PutUint16(buf[offset:offset+2], uint16(len(v)))
		offset += 2
		copy(buf[offset:], v)
		offset += len(v)
	}
	copy(buf[offset:], payloadBytes)
	crc := crc32IEEE(buf[:totalLen-4])
	binary.BigEndian.PutUint32(buf[totalLen-4:], crc)
	return buf
}

func TestParseEventFrame(t *testing.T) {
	frame := buildEventFrame(
		map[string]string{
			":event-type":   "assistantResponseEvent",
			":message-type": "event",
		},
		map[string]any{
			"assistantResponseEvent": map[string]any{"content": "hello"},
		},
	)

	parsed, err := parseEventFrame(frame)
	if err != nil {
		t.Fatalf("parseEventFrame failed: %v", err)
	}
	if got := parsed.Headers[":event-type"]; got != "assistantResponseEvent" {
		t.Errorf("event-type header = %q, want assistantResponseEvent", got)
	}
	if parsed.Payload == nil {
		t.Fatalf("payload nil")
	}
	inner := parsed.Payload["assistantResponseEvent"].(map[string]any)
	if inner["content"] != "hello" {
		t.Errorf("payload content = %v, want hello", inner["content"])
	}
}

func TestByteQueueRead(t *testing.T) {
	q := newByteQueue()
	q.push([]byte("hello"))
	q.push([]byte(" "))
	q.push([]byte("world"))

	if q.len() != 11 {
		t.Fatalf("queue length = %d, want 11", q.len())
	}

	got := q.read(6)
	if string(got) != "hello " {
		t.Errorf("read = %q, want \"hello \"", string(got))
	}
	if q.len() != 5 {
		t.Errorf("remaining length = %d, want 5", q.len())
	}

	got = q.read(10)
	if got != nil {
		t.Errorf("over-read should be nil, got %q", string(got))
	}
}

func TestByteQueueFrameExtraction(t *testing.T) {
	frame := buildEventFrame(map[string]string{
		":event-type": "messageStopEvent",
	}, map[string]any{"messageStopEvent": map[string]any{}})

	q := newByteQueue()
	// split frame across chunk boundary
	q.push(frame[:8])
	q.push(frame[8:])

	length, ok := q.peekUint32BE(0)
	if !ok || length != uint32(len(frame)) {
		t.Fatalf("peek length failed: ok=%v len=%d want %d", ok, length, len(frame))
	}
	data := q.read(int(length))
	if !bytes.Equal(data, frame) {
		t.Fatalf("extracted frame bytes mismatch")
	}
}
