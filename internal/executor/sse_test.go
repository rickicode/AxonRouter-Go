package executor

import (
	"testing"
)

func TestIsSSEDataLine(t *testing.T) {
	tests := []struct {
		name string
		line []byte
		want bool
	}{
		{"data line with json", []byte("data: {\"model\":\"gpt-4\"}"), true},
		{"data line with DONE", []byte("data: [DONE]"), true},
		{"data line with empty content", []byte("data: "), true},
		{"event line", []byte("event: message"), false},
		{"event line with colon", []byte("event: message:extra"), false},
		{"empty line", []byte(""), false},
		{"comment line", []byte(": ping"), false},
		{"id line", []byte("id: 1"), false},
		{"retry line", []byte("retry: 1000"), false},
		{"random text", []byte("hello world"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSSEDataLine(tt.line); got != tt.want {
				t.Errorf("IsSSEDataLine(%q) = %v, want %v", string(tt.line), got, tt.want)
			}
		})
	}
}

func TestParseSSEDataLine(t *testing.T) {
	tests := []struct {
		name string
		line []byte
		want []byte
	}{
		{"data line with json", []byte("data: {\"model\":\"gpt-4\"}"), []byte("{\"model\":\"gpt-4\"}")},
		{"data DONE marker", []byte("data: [DONE]"), nil},
		{"data line with empty content", []byte("data: "), []byte("")},
		{"event line", []byte("event: message"), nil},
		{"empty line", []byte(""), nil},
		{"comment line", []byte(": ping"), nil},
		{"id line", []byte("id: 1"), nil},
		{"retry line", []byte("retry: 1000"), nil},
		{"just data prefix", []byte("data"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSSEDataLine(tt.line)
			if tt.want == nil && got != nil {
				t.Errorf("ParseSSEDataLine(%q) = %v, want nil", string(tt.line), got)
				return
			}
			if tt.want != nil && got == nil {
				t.Errorf("ParseSSEDataLine(%q) = nil, want %v", string(tt.line), string(tt.want))
				return
			}
			if tt.want != nil && got != nil && string(got) != string(tt.want) {
				t.Errorf("ParseSSEDataLine(%q) = %q, want %q", string(tt.line), string(got), string(tt.want))
			}
		})
	}
}
