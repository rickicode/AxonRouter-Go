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
	"brave":             {DisplayName: "Brave Search", Aliases: nil},
	"tavily":            {DisplayName: "Tavily", Aliases: nil},
	"exa":               {DisplayName: "Exa", Aliases: nil},
	"jina":              {DisplayName: "Jina AI", Aliases: nil},
	"google-pse":        {DisplayName: "Google Programmable Search Engine", Aliases: []string{"google-pse-search"}},
	"firecrawl":         {DisplayName: "Firecrawl", Aliases: nil},
	"fal":               {DisplayName: "Fal.ai", Aliases: nil},
	"black-forest-labs": {DisplayName: "Black Forest Labs", Aliases: []string{"bfl"}},
	"assemblyai":        {DisplayName: "AssemblyAI", Aliases: nil},
	"cartesia":          {DisplayName: "Cartesia", Aliases: nil},
	"edge-tts":          {DisplayName: "Edge TTS", Aliases: nil},
	"qwen":              {DisplayName: "Qwen", Aliases: nil},
	"alicode":           {DisplayName: "AliCode", Aliases: nil},
	"kimi-coding":       {DisplayName: "Kimi Coding", Aliases: nil},
	"iflow":             {DisplayName: "iFlow", Aliases: nil},
	"volcengine-ark":    {DisplayName: "Volcengine Ark", Aliases: []string{"volcengine"}},
	"hunyuan":           {DisplayName: "Tencent Hunyuan", Aliases: nil},
	"nanobanana":        {DisplayName: "Nanobanana", Aliases: nil},
	"topaz":             {DisplayName: "Topaz", Aliases: nil},
	"puter":             {DisplayName: "Puter", Aliases: nil},
	"comfyui":           {DisplayName: "ComfyUI", Aliases: nil},
	"ag":                {DisplayName: "Antigravity", Aliases: nil},
	"cx":                {DisplayName: "OpenAI Codex", Aliases: nil},
	"kiro":              {DisplayName: "Kiro AI", Aliases: nil},
	"openai":            {DisplayName: "OpenAI Platform", Aliases: nil},
	"claude":            {DisplayName: "Anthropic Claude", Aliases: nil},
	"gemini":            {DisplayName: "Gemini", Aliases: nil},
	"deepseek":          {DisplayName: "DeepSeek", Aliases: nil},
	"groq":              {DisplayName: "Groq Cloud", Aliases: nil},
	"openrouter":        {DisplayName: "OpenRouter", Aliases: nil},
	"oc":                {DisplayName: "OpenCode Free", Aliases: []string{"opencode", "opencode-free"}},
	"oc-zen":            {DisplayName: "OpenCode Zen", Aliases: []string{"opencode-zen"}},
	"oc-go":             {DisplayName: "OpenCode Go", Aliases: []string{"opencode-go"}},
	"mimocode":          {DisplayName: "MiMoCode Free", Aliases: []string{"mimocode-free"}},
	"mimo":              {DisplayName: "Xiaomi MiMo PAYG", Aliases: nil},
	"mimo-tp":           {DisplayName: "MiMo Token Plan", Aliases: []string{"mimo-token"}},
	"cf":                {DisplayName: "Cloudflare Workers AI", Aliases: nil},
	"elevenlabs":        {DisplayName: "ElevenLabs", Aliases: nil},
	"deepgram":          {DisplayName: "DeepGram", Aliases: nil},
	"bedrock":           {DisplayName: "Amazon Bedrock Mantle", Aliases: nil},
	"devin":             {DisplayName: "Devin CLI", Aliases: nil},
	"qoder":             {DisplayName: "Qoder", Aliases: nil},
	"qwencloud":         {DisplayName: "Qwen Cloud", Aliases: nil},
	"codebuddy":         {DisplayName: "CodeBuddy", Aliases: []string{"codebuddy-cn"}},
	"zenmux-free":       {DisplayName: "ZenMux Free", Aliases: []string{"zxfree"}},
	"commandcode":       {DisplayName: "CommandCode AI", Aliases: []string{"cmd"}},
	"freebuff":          {DisplayName: "Freebuff", Aliases: []string{"fb"}},
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
