package caveman

import (
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/compression"
)

// rule maps matching substrings to replacements and tracks which technique they belong to.
type rule struct {
	pattern   string
	replace   string
	technique string
}

// builtInRules is the static English rule table (~80 rules across 13 categories).
// Rules are applied in order; later rules can overwrite earlier ones on overlap.
var builtInRules = []rule{
	// --- filler adverbs ---
	{"basically", "", "filler_adverbs"},
	{"essentially", "", "filler_adverbs"},
	{"actually", "", "filler_adverbs"},
	{"literally", "", "filler_adverbs"},
	{"simply", "", "filler_adverbs"},
	{"obviously", "", "filler_adverbs"},
	{"definitely", "", "filler_adverbs"},
	{"certainly", "", "filler_adverbs"},
	{"absolutely", "", "filler_adverbs"},
	{"frankly", "", "filler_adverbs"},
	{"honestly", "", "filler_adverbs"},
	{"seriously", "", "filler_adverbs"},

	// --- pleasantries ---
	{"sure", "", "pleasantries"},
	{"of course", "", "pleasantries"},
	{"happy to", "", "pleasantries"},
	{"no problem", "", "pleasantries"},
	{"you're welcome", "", "pleasantries"},
	{"my pleasure", "", "pleasantries"},
	{"glad to help", "", "pleasantries"},

	// --- hedging ---
	{"it seems like", "", "hedging"},
	{"it appears that", "", "hedging"},
	{"i think that", "", "hedging"},
	{"i believe that", "", "hedging"},
	{"probably", "", "hedging"},
	{"maybe it", "", "hedging"},
	{"it might be", "", "hedging"},
	{"in my opinion", "", "hedging"},
	{"from my perspective", "", "hedging"},

	// --- redundant phrasing ---
	{"make sure", "ensure", "redundant_phrasing"},
	{"be sure", "ensure", "redundant_phrasing"},
	{"due to the fact", "because", "redundant_phrasing"},
	{"the reason is because", "because", "redundant_phrasing"},
	{"in order to", "to", "redundant_phrasing"},
	{"with regard to", "about", "redundant_phrasing"},
	{"in relation to", "about", "redundant_phrasing"},
	{"for the purpose of", "for", "redundant_phrasing"},

	// --- redundant directives ---
	{"it is important", "", "redundant_directives"},
	{"remember to", "", "redundant_directives"},
	{"please note that", "", "redundant_directives"},
	{"it is worth noting", "", "redundant_directives"},
	{"it should be noted", "", "redundant_directives"},

	// --- verbose instructions ---
	{"provide a detailed", "", "verbose_instructions"},
	{"write an in-depth", "write", "verbose_instructions"},
	{"explain in detail", "explain", "verbose_instructions"},
	{"describe in detail", "describe", "verbose_instructions"},
	{"give a detailed", "give", "verbose_instructions"},
	{"comprehensive explanation", "explanation", "verbose_instructions"},
	{"detailed overview", "overview", "verbose_instructions"},

	// --- filler phrases ---
	{"i want to", "", "filler_phrases"},
	{"i need to", "", "filler_phrases"},
	{"i'd like to", "", "filler_phrases"},
	{"i would like to", "", "filler_phrases"},
	{"i just want to", "", "filler_phrases"},
	{"i just need to", "", "filler_phrases"},
	{"let me know if you need", "", "filler_phrases"},

	// --- redundant openers ---
	{"hi there", "", "redundant_openers"},
	{"hello", "", "redundant_openers"},
	{"hey", "", "redundant_openers"},
	{"good morning", "", "redundant_openers"},
	{"good afternoon", "", "redundant_openers"},
	{"good evening", "", "redundant_openers"},
	{"how are you", "", "redundant_openers"},
	{"how is it going", "", "redundant_openers"},

	// --- verbose requests ---
	{"i was wondering", "", "verbose_requests"},
	{"would it be possible", "can", "verbose_requests"},
	{"could you possibly", "can you", "verbose_requests"},
	{"do you think you could", "can you", "verbose_requests"},
	{"would you mind", "", "verbose_requests"},
	{"if you don't mind", "", "verbose_requests"},

	// --- leader phrases ---
	{"i'll", "", "leader_phrases"},
	{"let me", "", "leader_phrases"},
	{"i can", "", "leader_phrases"},
	{"you can", "", "leader_phrases"},
	{"we can", "", "leader_phrases"},
	{"allow me to", "", "leader_phrases"},

	// --- over acknowledgement ---
	{"as you mentioned", "", "over_acknowledgement"},
	{"as we discussed", "", "over_acknowledgement"},
	{"as you noted", "", "over_acknowledgement"},
	{"as previously stated", "", "over_acknowledgement"},
	{"as indicated", "", "over_acknowledgement"},

	// --- code intro phrases ---
	{"here is the code", "", "code_intro"},
	{"below is the code", "", "code_intro"},
	{"the following code", "", "code_intro"},
	{"here is the solution", "", "code_intro"},
	{"below is the solution", "", "code_intro"},
	{"the following solution", "", "code_intro"},

	// --- repeated explicit ---
	{"as you can see", "", "repeated_explicit"},
	{"as you may know", "", "repeated_explicit"},
	{"as you might imagine", "", "repeated_explicit"},
	{"as you are aware", "", "repeated_explicit"},
	{"as expected", "", "repeated_explicit"},
}

// compressText applies the Caveman rule table to a single text string.
// It preserves code blocks/URLs/JSON, applies rules to the rest, then restores.
func compressText(text string) (string, []string) {
	if text == "" {
		return text, nil
	}

	// Preserve blocks that should never be touched.
	remaining, blocks := compression.ExtractPreservedBlocks(text)
	lower := strings.ToLower(remaining)

	used := make(map[string]bool)
	changed := false

	for _, r := range builtInRules {
		pat := strings.ToLower(r.pattern)
		if !strings.Contains(lower, pat) {
			continue
		}
		// Replace on original-cased text using case-insensitive index search
		remaining = replaceCaseInsensitive(remaining, r.pattern, r.replace)
		lower = strings.ToLower(remaining)
		used[r.technique] = true
		changed = true
	}

	if !changed {
		return text, nil
	}

	// Trim extra spaces and empty sentences left behind by deletions.
	remaining = compression.CleanSpaces(remaining)
	if remaining == "" {
		// Fail-open: if compression empties the message, return original.
		return text, nil
	}

	result := compression.RestorePreservedBlocks(remaining, blocks)

	techniques := make([]string, 0, len(used))
	for t := range used {
		techniques = append(techniques, t)
	}
	return result, techniques
}

// replaceCaseInsensitive replaces all occurrences of pattern in s with repl,
// matching case-insensitively but preserving the replacement text as given.
func replaceCaseInsensitive(s, pattern, repl string) string {
	lowerS := strings.ToLower(s)
	lowerP := strings.ToLower(pattern)
	var out strings.Builder
	i := 0
	for {
		idx := strings.Index(lowerS[i:], lowerP)
		if idx == -1 {
			out.WriteString(s[i:])
			break
		}
		start := i + idx
		out.WriteString(s[i:start])
		out.WriteString(repl)
		i = start + len(pattern)
	}
	return out.String()
}
