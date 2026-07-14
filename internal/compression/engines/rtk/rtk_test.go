package rtk

import (
	"encoding/json"
	"strings"
	"testing"
)

func makeBody(messages any) []byte {
	var body map[string]any
	switch v := messages.(type) {
	case []any:
		body = map[string]any{"messages": v}
	default:
		body = map[string]any{"input": v}
	}
	b, _ := json.Marshal(body)
	return b
}

func TestEngineID(t *testing.T) {
	if Engine().ID() != "rtk" {
		t.Fatalf("expected engine ID rtk, got %s", Engine().ID())
	}
}

func TestEngineApply_InvalidJSONPassThrough(t *testing.T) {
	input := []byte(`not json`)
	out, stats, err := Engine().Apply(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(input) {
		t.Fatalf("expected original body on invalid JSON, got %s", string(out))
	}
	if stats.OriginalTokens == 0 {
		t.Error("expected original tokens on pass-through")
	}
}

func TestEngineApply_OpenAIToolString(t *testing.T) {
	text := "file.go:1:first match\nfile.go:2:second match\n" + strings.Repeat("file.go:3:repeat\n", 25)
	input := makeBody([]any{
		map[string]any{"role": "user", "content": "check this"},
		map[string]any{"role": "tool", "content": text},
	})
	out, stats, err := Engine().Apply(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if stats.OriginalTokens <= stats.CompressedTokens {
		t.Fatalf("expected RTK to reduce tokens: original=%d compressed=%d", stats.OriginalTokens, stats.CompressedTokens)
	}
	if !containsTechnique(stats.TechniquesUsed, "grep") {
		t.Fatalf("expected grep technique in %v", stats.TechniquesUsed)
	}
}

func TestEngineApply_OpenAIToolArray(t *testing.T) {
	text := "file.go:1:first match\n" + strings.Repeat("file.go:2:repeat\n", 30)
	input := makeBody([]any{
		map[string]any{
			"role": "tool",
			"content": []any{
				map[string]any{"type": "text", "text": text},
			},
		},
	})
	out, stats, err := Engine().Apply(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.OriginalTokens <= stats.CompressedTokens {
		t.Fatalf("expected RTK to reduce tokens: original=%d compressed=%d", stats.OriginalTokens, stats.CompressedTokens)
	}
	if !containsTechnique(stats.TechniquesUsed, "grep") {
		t.Fatalf("expected grep technique in %v", stats.TechniquesUsed)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestEngineApply_ClaudetoolResultString(t *testing.T) {
	text := "On branch main\n\nmodified: a.go\n" + strings.Repeat("modified: b.go\n", 15)
	input := makeBody([]any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":    "tool_result",
					"content": text,
				},
			},
		},
	})
	out, stats, err := Engine().Apply(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.OriginalTokens <= stats.CompressedTokens {
		t.Fatalf("expected RTK to reduce tokens: original=%d compressed=%d", stats.OriginalTokens, stats.CompressedTokens)
	}
	if !containsTechnique(stats.TechniquesUsed, "git-status") {
		t.Fatalf("expected git-status technique in %v", stats.TechniquesUsed)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestEngineApply_ClaudetoolResultArray(t *testing.T) {
	text := "diff --git a/a.go b/a.go\n" + strings.Repeat("+line\n", 200)
	input := makeBody([]any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "tool_result",
					"content": []any{
						map[string]any{"type": "text", "text": text},
					},
				},
			},
		},
	})
	out, stats, err := Engine().Apply(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.OriginalTokens <= stats.CompressedTokens {
		t.Fatalf("expected RTK to reduce tokens: original=%d compressed=%d", stats.OriginalTokens, stats.CompressedTokens)
	}
	if !containsTechnique(stats.TechniquesUsed, "git-diff") {
		t.Fatalf("expected git-diff technique in %v", stats.TechniquesUsed)
	}
	if strings.Count(string(out), "+line") > 120 {
		t.Fatal("expected git-diff hunk truncation")
	}
}

func TestEngineApply_ResponsesFunctionCallOutput(t *testing.T) {
	text := "./a/b/c.go\n./a/b/d.go\n" + strings.Repeat("./x/y.go\n", 40)
	input := makeBody([]any{
		map[string]any{"type": "function_call_output", "output": text},
	})
	out, stats, err := Engine().Apply(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.OriginalTokens <= stats.CompressedTokens {
		t.Fatalf("expected RTK to reduce tokens: original=%d compressed=%d", stats.OriginalTokens, stats.CompressedTokens)
	}
	if !containsTechnique(stats.TechniquesUsed, "find") {
		t.Fatalf("expected find technique in %v", stats.TechniquesUsed)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestEngineApply_ResponsesFunctionCallOutputArray(t *testing.T) {
	text := strings.Repeat("npm warn deprecated\n", 10) + "npm error something\n"
	input := makeBody([]any{
		map[string]any{
			"type": "function_call_output",
			"output": []any{
				map[string]any{"type": "input_text", "text": text},
			},
		},
	})
	out, stats, err := Engine().Apply(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.OriginalTokens <= stats.CompressedTokens {
		t.Fatalf("expected RTK to reduce tokens: original=%d compressed=%d", stats.OriginalTokens, stats.CompressedTokens)
	}
	if !containsTechnique(stats.TechniquesUsed, "build-output") {
		t.Fatalf("expected build-output technique in %v", stats.TechniquesUsed)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestEngineApply_SmallContentUnchanged(t *testing.T) {
	input := makeBody([]any{
		map[string]any{"role": "tool", "content": "a short string"},
	})
	out, _, _ := Engine().Apply(input, nil)
	if !strings.Contains(string(out), "a short string") {
		t.Fatalf("expected small content to remain unchanged, got %s", string(out))
	}
}

func TestEngineApply_UserMessageUnchanged(t *testing.T) {
	text := "file.go:1:first match\n" + strings.Repeat("file.go:2:repeat\n", 30)
	input := makeBody([]any{
		map[string]any{"role": "user", "content": text},
	})
	out, _, err := Engine().Apply(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "file.go") {
		t.Fatalf("expected user message to remain unchanged, got %s", string(out))
	}
}

func TestFilterGrep(t *testing.T) {
	input := "a.go:1:hello\na.go:2:world\nb.go:1:foo\n" + strings.Repeat("c.go:5:dup\n", 15)
	out := filterGrep(input)
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "b.go") {
		t.Fatalf("expected grouped grep output, got %s", out)
	}
	if strings.Contains(out, "c.go:5:dup") {
		t.Fatalf("expected capped matches for c.go")
	}
}

func TestFilterGitDiff(t *testing.T) {
	input := "diff --git a/f.go b/f.go\n@@ -1,5 +1,200 @@\n" + strings.Repeat("+x\n", 250)
	out := filterGitDiff(input)
	if strings.Count(out, "+x") > 120 {
		t.Fatalf("expected hunk truncation, got %d +x lines", strings.Count(out, "+x"))
	}
}

func TestFilterGitStatus(t *testing.T) {
	input := "On branch main\n\nChanges to be committed:\n  (use \"git restore --staged ...\" to unstage)\n\n\tnew file:   a.go\n"
	out := filterGitStatus(input)
	if !strings.Contains(out, "main") {
		t.Fatalf("expected branch in git-status output, got %s", out)
	}
}

func TestFilterFind(t *testing.T) {
	input := "./a/1.go\n./a/2.go\n./b/3.go\n"
	out := filterFind(input)
	if !strings.Contains(out, "a/") || !strings.Contains(out, "b/") {
		t.Fatalf("expected grouped find output, got %s", out)
	}
}

func TestFilterLs(t *testing.T) {
	input := `total 16
drwxr-xr-x 2 user user 4096 Jan 1 00:00 a
drwxr-xr-x 2 user user 4096 Jan 1 00:00 b
-rw-r--r-- 1 user user  123 Jan 1 00:00 x.go
`
	out := filterLs(input)
	if !strings.Contains(out, "a/") || !strings.Contains(out, "x.go") {
		t.Fatalf("expected ls summary output, got %s", out)
	}
}

func TestFilterTree(t *testing.T) {
	input := "a\n├── b\n└── c\n\n1 directory, 2 files\n"
	out := filterTree(input)
	if strings.Contains(out, "directory") {
		t.Fatalf("expected tree summary removed, got %s", out)
	}
}

func TestFilterDedupLog(t *testing.T) {
	input := "a\na\na\nb\n\nb\n"
	out := filterDedupLog(input)
	if strings.Count(out, "a\n") > 2 {
		t.Fatalf("expected duplicate collapse for a, got %s", out)
	}
}

func TestFilterSmartTruncate(t *testing.T) {
	input := strings.Repeat("line\n", 300)
	out := filterSmartTruncate(input)
	if strings.Count(out, "line") >= 250 {
		t.Fatalf("expected smart truncation, got %s", out)
	}
}

func TestFilterReadNumbered(t *testing.T) {
	var lines []string
	for i := 1; i <= 300; i++ {
		lines = append(lines, strings.Repeat(" ", 4-len(itoa(i)))+itoa(i)+"|content")
	}
	input := strings.Join(lines, "\n")
	out := filterReadNumbered(input)
	if strings.Count(out, "content") >= 250 {
		t.Fatalf("expected read-numbered truncation, got %s", out)
	}
}

func TestFilterSearchList(t *testing.T) {
	input := "Result of search in 'src' (total 10 files):\n- ./src/a.go\n- ./src/b.go\n"
	out := filterSearchList(input)
	if !strings.Contains(out, "src") || !strings.Contains(out, "a.go") {
		t.Fatalf("expected search-list compression, got %s", out)
	}
}

func containsTechnique(techs []string, want string) bool {
	for _, t := range techs {
		if t == want {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var s string
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
