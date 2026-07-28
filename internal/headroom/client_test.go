package headroom

import (
	"encoding/json"
	"testing"
)

func TestCompressibleToolText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"string", `"hello world"`, "hello world"},
		{"array", `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, "a\nb"},
		{"object", `{"type":"text","text":"inside"}`, "inside"},
		{"null", `null`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CompressibleToolText(json.RawMessage(tc.in))
			if string(got) != tc.want {
				t.Fatalf("got %q want %q", string(got), tc.want)
			}
		})
	}
}
