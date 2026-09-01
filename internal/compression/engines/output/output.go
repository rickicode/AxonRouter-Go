package output

import (
	"encoding/json"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/compression"
)

func init() {
	compression.Register(&Engine{})
}

// Output-shaping system prompts. The caveman set has 6 levels (lite/full/ultra
// plus wenyan classical-Chinese variants) and the ponytail set has 3 levels
// (lite/full/ultra), matching 9router's cavemanPrompts.js / ponytailPrompt.js.
const sharedCavemanBoundaries = "Code blocks, file paths, commands, errors, URLs: keep exact. Security warnings, irreversible action confirmations, multi-step ordered sequences: write normal. Resume terse style after."
const sharedCavemanExamples = "Not: \"Sure! I'd be happy to help you with that.\" Yes: \"Bug in auth middleware. Token expiry check use `<` not `<=`. Fix:\""
const sharedCavemanAutoClarity = "Auto-Clarity: drop caveman for security warnings, irreversible actions, multi-step sequences where fragment ambiguity risks misread, or when user repeats a question. Resume after the clear part."
const sharedCavemanPersistence = "ACTIVE EVERY RESPONSE. No revert after many turns. No filler drift. Still active if unsure."
const sharedCavemanNoInventedAbbrev = "No invented abbreviations. Standard well-known tech acronyms (DB, API, HTTP, URL, JSON, ID, OS, CPU) OK. Names of code symbols, function names, API names, error strings: keep verbatim."
const sharedCavemanPreserveLanguage = "Preserve the user's dominant language. User wrote Vietnamese, reply Vietnamese. User wrote English, reply English. Wenyan/classical-Chinese levels override this language-preservation rule. Code identifiers, error strings, file paths, commands: keep in their original form regardless of language."
const sharedCavemanNoSelfReference = `No self-reference. Do not name or announce the style (no "caveman mode", no "me caveman think", no "compressed mode active"). Just respond.`
const sharedCavemanNoDecoration = `No decorative emoji. No narrating tool calls ("I will now search", "I used X to find Y"). No status phrases ("Sure!", "Of course!", "I'd be happy to"). No causal arrow shorthand ("A -> B -> fails"). State the thing, the action, the reason. Then next step.`

const promptCaveman = "Be extremely terse. Use the fewest words possible. Prefer code, data, and bullet points over prose. Drop articles, hedging, filler words, and explanations unless asked. Never apologize or restate the question."

const promptPonytail = "Output only the minimal code or changes needed. Do not add comments, examples, explanations, docstrings, tests, or imports unless explicitly requested. Follow YAGNI: if something is not required, omit it."

const sharedPonytailPersona = "You are a lazy senior developer. Lazy means efficient, not careless. The best code is the code never written."
const sharedPonytailLadder = "Before writing code, stop at the first rung that holds: 1) Does this need to exist at all? (YAGNI) 2) Stdlib does it? Use it. 3) Native platform feature covers it? Use it (CSS over JS, DB constraint over app code). 4) Already-installed dependency solves it? Use it; never add a new one for what a few lines can do. 5) Can it be one line? One line. 6) Only then: the minimum code that works."
const sharedPonytailRules = "No unrequested abstractions (no interface with one implementation, no factory for one product, no config for a value that never changes). No boilerplate or scaffolding \"for later\". Deletion over addition. Boring over clever. Fewest files possible; shortest working diff wins. Two stdlib options the same size: take the edge-case-correct one. Mark deliberate simplifications with a `ponytail:` comment naming the ceiling and upgrade path."
const sharedPonytailOutput = "Code first. Then at most three short lines: what was skipped, when to add it. No essays or design notes. Pattern: `[code] → skipped: [X], add when [Y].`"
const sharedPonytailNotLazy = "Never simplify away: input validation at trust boundaries, error handling that prevents data loss, security, accessibility, anything explicitly requested. Non-trivial logic leaves ONE runnable check behind (an assert-based self-check or one small test file; no frameworks). Trivial one-liners need no test."
const sharedPonytailPersistence = "ACTIVE EVERY RESPONSE. No drift back to over-building. Still active if unsure."

// Engine injects an output-side system prompt into OpenAI-style request bodies.
// It is fail-open: any parse error returns the unmodified body.
type Engine struct{}

// ID returns the engine identifier.
func (e *Engine) ID() string { return "output" }

// Apply injects a terse/YAGNI system prompt into the first available system
// message, or prepends a new system message when none exists. Unparseable or
// unsupported bodies are returned unchanged.
func (e *Engine) Apply(body []byte, config compression.EngineConfig) ([]byte, compression.EngineStats, error) {
	original := string(body)
	beforeTokens := compression.EstimateTokens(original)

	level, _ := config["level"].(string)
	prompt, ok := promptForLevel(level)
	if !ok {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	messages, _ := m["messages"].([]any)
	if messages == nil {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	changed := false
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "system" {
			continue
		}
		changed = injectPrompt(msg, prompt)
		break // only touch the first system message
	}

	if !changed {
		// No existing system message — prepend one.
		messages = append([]any{map[string]any{"role": "system", "content": prompt}}, messages...)
		changed = true
	}

	if !changed {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	m["messages"] = messages
	outBody, err := json.Marshal(m)
	if err != nil {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	afterTokens := compression.EstimateTokens(string(outBody))
	technique := "output_" + level

	return outBody, compression.EngineStats{
		OriginalTokens:   beforeTokens,
		CompressedTokens: afterTokens,
		SavingsPercent:   savingsPercent(beforeTokens, afterTokens),
		TechniquesUsed:   []string{technique},
	}, nil
}

// Level-specific directives. The bare level names ("caveman", "ponytail") map
// to the full variants for backward compatibility.
var cavemanLevelDirective = map[string]string{
	"caveman-lite": "Caveman terse. Fewest words. Code, data, bullets. No filler.",
	"caveman-full": promptCaveman,
	"caveman":      promptCaveman,
	"caveman-ultra": "ULTRA caveman. Absolute minimum: no sentences where a fragment does, no articles, no verbs where context carries. Headline only. Readable by a tired engineer at 3am.",
}

var ponytailLevelDirective = map[string]string{
	"ponytail-lite": "Lazy but warm. Minimal code, skip unrequested extras, but keep tone natural.",
	"ponytail-full": promptPonytail,
	"ponytail":      promptPonytail,
	"ponytail-ultra": "ULTRA lazy. If a line of code, config, or dependency can be avoided, avoid it. If the whole feature can be one file, one file. Every line you do not write is a line you do not maintain.",
}

// wenyanLevelDirective holds classical-Chinese output-shaping directives. The
// wenyan style responds in 文言文 (classical Chinese) — terse, allusive, poetic.
// These override the preserve-language rule (as in 9router's cavemanPrompts).
var wenyanLevelDirective = map[string]string{
	"wenyan-lite": "用文言作答。辞约而旨丰，勿以白话铺陈。代码、路径、错误字符串保持原样。",
	"wenyan":      "以文言文作答，如古文笔法。字斟句酌，言简意赅，去繁就简。技术符号、代码、文件路径、错误信息皆保持原文，不得译改。",
	"wenyan-ultra": "以极简文言作答，仿先秦诸子之笔。一字千金，去尽虚词。惟代码、符号、路径、错误串仍用原文。",
}

func promptForLevel(level string) (string, bool) {
	switch level {
	case "caveman", "caveman-lite", "caveman-full", "caveman-ultra":
		return buildCavemanPrompt(level), true
	case "wenyan-lite", "wenyan", "wenyan-ultra":
		return buildWenyanPrompt(level), true
	case "ponytail", "ponytail-lite", "ponytail-full", "ponytail-ultra":
		return buildPonytailPrompt(level), true
	default:
		return "", false
	}
}

// buildCavemanPrompt assembles the full caveman system prompt from the shared
// fragments, mirroring 9router's cavemanPrompts.js composition order.
func buildCavemanPrompt(level string) string {
	directive := cavemanLevelDirective[level]
	if directive == "" {
		directive = promptCaveman
	}
	parts := []string{
		directive,
		sharedCavemanBoundaries,
		sharedCavemanExamples,
		sharedCavemanNoInventedAbbrev,
		sharedCavemanNoSelfReference,
		sharedCavemanNoDecoration,
		sharedCavemanPreserveLanguage,
	}
	if level == "caveman-ultra" {
		parts = append(parts, sharedCavemanAutoClarity)
	}
	parts = append(parts, sharedCavemanPersistence)
	return joinParts(parts)
}

// buildWenyanPrompt assembles a classical-Chinese output prompt. Wenyan levels
// override the preserve-language rule, so the shared preserve-language fragment
// is omitted here.
func buildWenyanPrompt(level string) string {
	parts := []string{
		wenyanLevelDirective[level],
		sharedCavemanBoundaries,
		sharedCavemanExamples,
		sharedCavemanNoInventedAbbrev,
		sharedCavemanNoSelfReference,
		sharedCavemanNoDecoration,
		sharedCavemanPersistence,
	}
	return joinParts(parts)
}

// buildPonytailPrompt assembles the full ponytail system prompt, mirroring
// 9router's ponytailPrompt.js composition order (persona, ladder, rules,
// output, not-lazy, persistence).
func buildPonytailPrompt(level string) string {
	directive := ponytailLevelDirective[level]
	if directive == "" {
		directive = promptPonytail
	}
	parts := []string{
		directive,
		sharedPonytailPersona,
		sharedPonytailLadder,
		sharedPonytailRules,
		sharedPonytailOutput,
	}
	// lite skips the not-lazy guardrails; full/ultra include them.
	if level != "ponytail-lite" {
		parts = append(parts, sharedPonytailNotLazy)
	}
	parts = append(parts, sharedPonytailPersistence)
	return joinParts(parts)
}

func joinParts(parts []string) string {
	var b strings.Builder
	first := true
	for _, p := range parts {
		if p == "" {
			continue
		}
		if !first {
			b.WriteString("\n\n")
		}
		b.WriteString(p)
		first = false
	}
	return b.String()
}

func injectPrompt(msg map[string]any, prompt string) bool {
	content := msg["content"]
	switch v := content.(type) {
	case string:
		if v == "" {
			msg["content"] = prompt
		} else {
			msg["content"] = v + "\n\n" + prompt
		}
		return true
	case []any:
		for _, partRaw := range v {
			part, ok := partRaw.(map[string]any)
			if !ok || part["type"] != "text" {
				continue
			}
			text, _ := part["text"].(string)
			if text == "" {
				part["text"] = prompt
			} else {
				part["text"] = text + "\n\n" + prompt
			}
			return true
		}
		// No text part found — append a text part with the prompt.
		msg["content"] = append(v, map[string]any{"type": "text", "text": prompt})
		return true
	}
	return false
}

func savingsPercent(before, after int) float64 {
	if before <= 0 {
		return 0.0
	}
	return (1.0 - float64(after)/float64(before)) * 100
}
