package chat

import (
	"strings"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	cases := []struct {
		name, input, want string
		invalid           bool
	}{
		{"trims", "  Ada  ", "Ada", false},
		{"unicode boundary", strings.Repeat("界", 32), strings.Repeat("界", 32), false},
		{"unicode too long", strings.Repeat("界", 33), "", true},
		{"blank", " \n\t ", "", true},
		{"invalid utf8", string([]byte{0xff}), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeName(tc.input)
			if (err != nil) != tc.invalid || (!tc.invalid && got != tc.want) {
				t.Fatalf("got %q, %v; want %q, invalid=%v", got, err, tc.want, tc.invalid)
			}
		})
	}
}
func TestNormalizeContent(t *testing.T) {
	cases := []struct {
		name, input, want string
		invalid           bool
	}{
		{"trims", " hello\nworld ", "hello\nworld", false},
		{"unicode boundary", strings.Repeat("😀", 1000), strings.Repeat("😀", 1000), false},
		{"unicode too long", strings.Repeat("😀", 1001), "", true},
		{"blank", " \n\t ", "", true},
		{"invalid utf8", string([]byte{0xff}), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeContent(tc.input)
			if (err != nil) != tc.invalid || (!tc.invalid && got != tc.want) {
				t.Fatalf("got len=%d, %v; want invalid=%v", len(got), err, tc.invalid)
			}
		})
	}
}

func TestDecodeCommand(t *testing.T) {
	cases := []struct{ name, payload, code, content string }{
		{"valid", `{"type":"message.send","content":" hello\nworld ","senderName":"forged"}`, "", "hello\nworld"},
		{"malformed", `{`, "invalid_command", ""},
		{"array", `[]`, "invalid_command", ""},
		{"null", `null`, "invalid_command", ""},
		{"missing type", `{"content":"hi"}`, "unsupported_type", ""},
		{"unsupported type", `{"type":"presence.updated","content":"hi"}`, "unsupported_type", ""},
		{"wrong type", `{"type":1,"content":"hi"}`, "invalid_command", ""},
		{"wrong content type", `{"type":"message.send","content":1}`, "invalid_command", ""},
		{"blank", `{"type":"message.send","content":" "}`, "invalid_content", ""},
		{"missing content", `{"type":"message.send"}`, "invalid_content", ""},
		{"invalid utf8", string([]byte{'{', '"', 0xff, '"', ':', '1', '}'}), "invalid_command", ""},
		{"trailing value", `{"type":"message.send","content":"hi"} {}`, "invalid_command", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			command, err := DecodeCommand([]byte(tc.payload))
			if tc.code == "" {
				if err != nil || command.Content != tc.content || command.Type != "message.send" {
					t.Fatalf("command %+v, error %v", command, err)
				}
				return
			}
			protocolErr, ok := err.(*Error)
			if !ok || protocolErr.Code != tc.code {
				t.Fatalf("got %v, want %s", err, tc.code)
			}
		})
	}
}
