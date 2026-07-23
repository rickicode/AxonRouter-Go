package provider

// CanonicalInfo holds a provider's display name and its legacy aliases.
// This is the single source of truth for alias resolution; every canonical
// provider ID must have an entry here.
type CanonicalInfo struct {
	DisplayName string   `json:"display_name"`
	Aliases     []string `json:"aliases"`
}

// Registry maps canonical short ID → metadata + legacy aliases.
// Aliases must only resolve to canonical IDs that exist in provider_types.
var Registry = map[string]CanonicalInfo{
	"ag":          {DisplayName: "Antigravity", Aliases: nil},
	"cx":          {DisplayName: "OpenAI Codex", Aliases: nil},
	"kiro":        {DisplayName: "Kiro AI", Aliases: nil},
	"amazon-q":    {DisplayName: "Amazon Q", Aliases: []string{"aq"}},
	"openai":      {DisplayName: "OpenAI Platform", Aliases: nil},
	"claude":      {DisplayName: "Anthropic Claude", Aliases: nil},
	"gemini":      {DisplayName: "Gemini", Aliases: nil},
	"deepseek":    {DisplayName: "DeepSeek", Aliases: nil},
	"groq":        {DisplayName: "Groq Cloud", Aliases: nil},
	"openrouter":  {DisplayName: "OpenRouter", Aliases: nil},
	"oc":          {DisplayName: "OpenCode Free", Aliases: []string{"opencode", "opencode-free"}},
	"oc-zen":      {DisplayName: "OpenCode Zen", Aliases: []string{"opencode-zen"}},
	"oc-go":       {DisplayName: "OpenCode Go", Aliases: []string{"opencode-go"}},
	"mimocode":    {DisplayName: "MiMoCode Free", Aliases: []string{"mimocode-free"}},
	"mimo":        {DisplayName: "Xiaomi MiMo PAYG", Aliases: nil},
	"mimo-tp":     {DisplayName: "MiMo Token Plan", Aliases: []string{"mimo-token"}},
	"cf":          {DisplayName: "Cloudflare Workers AI", Aliases: nil},
	"elevenlabs":  {DisplayName: "ElevenLabs", Aliases: nil},
	"deepgram":    {DisplayName: "DeepGram", Aliases: nil},
	"bedrock":     {DisplayName: "Amazon Bedrock Mantle", Aliases: nil},
	"devin":       {DisplayName: "Devin CLI", Aliases: nil},
	"qoder":       {DisplayName: "Qoder", Aliases: nil},
	"qwencloud":   {DisplayName: "Qwen Cloud", Aliases: nil},
	"codebuddy":   {DisplayName: "CodeBuddy", Aliases: []string{"codebuddy-cn"}},
	"zenmux-free": {DisplayName: "ZenMux Free", Aliases: []string{"zxfree"}},
}

// aliasToCanonical is the flattened reverse lookup (built once at init).
var aliasToCanonical map[string]string

func init() {
	aliasToCanonical = make(map[string]string, len(Registry)*2)
	for canonical, info := range Registry {
		aliasToCanonical[canonical] = canonical
		for _, alias := range info.Aliases {
			aliasToCanonical[alias] = canonical
		}
	}
}

// ResolveAlias converts a legacy or alias provider ID to its canonical form.
func ResolveAlias(id string) string {
	if canonical, ok := aliasToCanonical[id]; ok {
		return canonical
	}
	return id
}
