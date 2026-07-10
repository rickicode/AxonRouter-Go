// Static provider catalog. Merged with runtime connection data from API.
// Category labels and ordering mirror OmniRoute provider catalog sections.

export interface ProviderCategory {
  id: string;
  label: string;
  color: string;
  description: string;
}

export interface ProviderMeta {
  id: string;
  displayName: string;
  icon: string;
  textIcon: string;
  iconFile?: string;
  category: string;
  description: string;
  format: string;
  authType: 'none' | 'apikey' | 'oauth' | 'custom';
  prefix: string;
  isBuiltIn: boolean;
  website?: string;
  color: string;
  serviceKinds?: string[];
  hasFree?: boolean;
  freeNote?: string;
  authHint?: string;
  apiHint?: string;
  inputFormat?: string;
}

export const CATEGORIES: ProviderCategory[] = [
  { id: 'oauth', label: 'OAuth', color: '#3b82f6', description: 'Account and CLI subscription providers that authenticate with OAuth.' },
  { id: 'ide', label: 'IDE', color: '#06b6d4', description: 'Editors with built-in AI subscriptions and imported credentials.' },
  { id: 'free', label: 'Free tier', color: '#22c55e', description: 'Providers with a free or no-auth access path.' },
  { id: 'no-auth', label: 'No auth', color: '#78716c', description: 'Keyless public endpoints or bootstrap-token providers.' },
  { id: 'upstream-proxy', label: 'Upstream proxy', color: '#6366f1', description: 'Proxy and relay surfaces that forward to another upstream.' },
  { id: 'apikey', label: 'API key', color: '#f59e0b', description: 'Managed providers that use bearer API keys.' },
  { id: 'compatible', label: 'Compatible', color: '#f97316', description: 'OpenAI, Anthropic, or Claude Code compatible custom endpoints.' },
  { id: 'web-cookie', label: 'Web cookie', color: '#a855f7', description: 'Browser session, cookie, or web-token based providers.' },
  { id: 'search', label: 'Search', color: '#14b8a6', description: 'Search API providers.' },
  { id: 'webfetch', label: 'Web fetch', color: '#fb923c', description: 'Reader and fetch providers for web content extraction.' },
  { id: 'audio', label: 'Audio', color: '#f43f5e', description: 'Speech-to-text and text-to-speech providers.' },
  { id: 'local', label: 'Local', color: '#10b981', description: 'Local or self-hosted model runtimes.' },
  { id: 'cloud-agent', label: 'Cloud agent', color: '#8b5cf6', description: 'Hosted coding agents and remote execution surfaces.' },
];

export const PROVIDER_CATALOG: ProviderMeta[] = [
  {
    id: 'codex',
    displayName: 'OpenAI Codex',
    icon: 'code',
    textIcon: 'CX',
    iconFile: '/providers/codex.svg',
    category: 'oauth',
    description: 'OpenAI Codex account routed through the OAuth device-code flow.',
    format: 'openai-responses',
    authType: 'oauth',
    prefix: 'cx/',
    isBuiltIn: true,
    website: 'https://openai.com',
    color: '#3b82f6',
    serviceKinds: ['llm'],
  },
  {
    id: 'antigravity',
    displayName: 'Antigravity',
    icon: 'rocket_launch',
    textIcon: 'AG',
    iconFile: '/providers/antigravity.png',
    category: 'oauth',
    description: 'Google Antigravity account provider with OAuth-backed routing.',
    format: 'antigravity',
    authType: 'oauth',
    prefix: 'ag/',
    isBuiltIn: true,
    website: 'https://antigravity.google',
    color: '#f59e0b',
    serviceKinds: ['llm'],
  },
  {
    id: 'kiro',
    displayName: 'Kiro AI',
    icon: 'psychology_alt',
    textIcon: 'KR',
    iconFile: '/providers/kiro.svg',
    category: 'oauth',
    description: 'Kiro AI provider using AWS Builder ID style OAuth credentials.',
    format: 'kiro',
    authType: 'oauth',
    prefix: 'kiro/',
    isBuiltIn: true,
    color: '#ff6b35',
    serviceKinds: ['llm'],
    hasFree: true,
    freeNote: 'Free monthly credit tier when eligible on the upstream account.',
  },
  {
    id: 'claude',
    displayName: 'Anthropic Claude',
    icon: 'smart_toy',
    textIcon: 'AN',
    iconFile: '/providers/anthropic-m.png',
    category: 'apikey',
    description: 'Anthropic Claude API provider using an API key.',
    format: 'anthropic',
    authType: 'apikey',
    prefix: 'claude/',
    isBuiltIn: true,
    website: 'https://anthropic.com',
    color: '#d97757',
    serviceKinds: ['llm'],
  },
  {
    id: 'openai',
    displayName: 'OpenAI',
    icon: 'auto_awesome',
    textIcon: 'OA',
    iconFile: '/providers/openai.png',
    category: 'apikey',
    description: 'OpenAI platform API key provider for GPT and reasoning models.',
    format: 'openai',
    authType: 'apikey',
    prefix: 'openai/',
    isBuiltIn: true,
    website: 'https://platform.openai.com',
    color: '#10a37f',
    serviceKinds: ['llm'],
  },
  {
    id: 'gemini',
    displayName: 'Gemini',
    icon: 'diamond',
    textIcon: 'GE',
    iconFile: '/providers/gemini-cli.svg',
    category: 'apikey',
    description: 'Google AI Studio Gemini models through API key authentication.',
    format: 'gemini',
    authType: 'apikey',
    prefix: 'gemini/',
    isBuiltIn: true,
    website: 'https://ai.google.dev',
    color: '#4285f4',
    serviceKinds: ['llm'],
    hasFree: true,
    freeNote: 'Google AI Studio free quota depends on model and region.',
  },
  {
    id: 'deepseek',
    displayName: 'DeepSeek',
    icon: 'bolt',
    textIcon: 'DS',
    iconFile: '/providers/deepseek.png',
    category: 'apikey',
    description: 'DeepSeek API key provider using OpenAI-compatible chat endpoints.',
    format: 'openai',
    authType: 'apikey',
    prefix: 'deepseek/',
    isBuiltIn: true,
    website: 'https://platform.deepseek.com',
    color: '#4d6bfe',
    serviceKinds: ['llm'],
    hasFree: true,
    freeNote: 'Free token grants are controlled by the upstream DeepSeek account.',
  },
  {
    id: 'groq',
    displayName: 'Groq',
    icon: 'speed',
    textIcon: 'GQ',
    iconFile: '/providers/groq.png',
    category: 'apikey',
    description: 'Groq low-latency inference for hosted LLM models.',
    format: 'openai',
    authType: 'apikey',
    prefix: 'groq/',
    isBuiltIn: true,
    website: 'https://groq.com',
    color: '#f55036',
    serviceKinds: ['llm'],
    hasFree: true,
    freeNote: 'Groq free tier limits depend on model and account state.',
  },
  {
    id: 'openrouter',
    displayName: 'OpenRouter',
    icon: 'router',
    textIcon: 'OR',
    iconFile: '/providers/openrouter.png',
    category: 'apikey',
    description: 'OpenRouter multi-model gateway using OpenAI-compatible API keys.',
    format: 'openai',
    authType: 'apikey',
    prefix: 'openrouter/',
    isBuiltIn: true,
    website: 'https://openrouter.ai',
    color: '#f97316',
    serviceKinds: ['llm'],
    hasFree: true,
    freeNote: 'Free models are available upstream with model-specific limits.',
  },
  {
    id: 'mimo',
    displayName: 'Xiaomi MiMo PAYG',
    icon: 'devices',
    textIcon: 'MM',
    iconFile: '/providers/mimo.svg',
    category: 'apikey',
    description: 'Xiaomi MiMo pay-as-you-go model endpoint.',
    format: 'openai',
    authType: 'apikey',
    prefix: 'mimo/',
    isBuiltIn: true,
    website: 'https://mimo.mi.com',
    color: '#ea580c',
    serviceKinds: ['llm'],
  },
  {
    id: 'mimo-tp',
    displayName: 'Xiaomi MiMo Token Plan',
    icon: 'devices',
    textIcon: 'MM',
    iconFile: '/providers/mimo.svg',
    category: 'apikey',
    description: 'MiMo monthly token-plan connection pool for high-volume routing.',
    format: 'openai',
    authType: 'apikey',
    prefix: 'mimo-tp/',
    isBuiltIn: true,
    website: 'https://mimo.mi.com',
    color: '#ea580c',
    serviceKinds: ['llm'],
  },
  {
    id: 'zai',
    displayName: 'Z.ai GLM',
    icon: 'psychology',
    textIcon: 'ZA',
    iconFile: '/providers/glm.png',
    category: 'apikey',
    description: 'Z.ai GLM endpoint exposed through Claude-compatible messages.',
    format: 'claude',
    authType: 'apikey',
    prefix: 'zai/',
    isBuiltIn: false,
    website: 'https://z.ai',
    color: '#2563eb',
    serviceKinds: ['llm'],
  },
  {
    id: 'oc-zen',
    displayName: 'OpenCode Zen',
    icon: 'opencode',
    textIcon: 'OZ',
    iconFile: '/providers/opencode-zen.png',
    category: 'apikey',
    description: 'OpenCode Zen API-key tier exposed as a gateway provider.',
    format: 'openai',
    authType: 'apikey',
    prefix: 'oc-zen/',
    isBuiltIn: true,
    website: 'https://opencode.ai/zen',
    color: '#6366f1',
    serviceKinds: ['llm'],
    hasFree: true,
  },
  {
    id: 'oc-go',
    displayName: 'OpenCode Go',
    icon: 'opencode',
    textIcon: 'OG',
    iconFile: '/providers/opencode-go.png',
    category: 'apikey',
    description: 'OpenCode Go API-key tier for Qwen-oriented routing.',
    format: 'openai',
    authType: 'apikey',
    prefix: 'oc-go/',
    isBuiltIn: true,
    website: 'https://opencode.ai/go',
    color: '#6366f1',
    serviceKinds: ['llm'],
    hasFree: true,
  },
  {
    id: 'elevenlabs',
    displayName: 'ElevenLabs',
    icon: 'record_voice_over',
    textIcon: 'EL',
    iconFile: '/providers/elevenlabs.png',
    category: 'audio',
    description: 'ElevenLabs text-to-speech synthesis provider.',
    format: 'openai',
    authType: 'apikey',
    prefix: 'elevenlabs/',
    isBuiltIn: true,
    website: 'https://elevenlabs.io',
    color: '#6c47ff',
    serviceKinds: ['tts'],
  },
  {
    id: 'deepgram',
    displayName: 'Deepgram',
    icon: 'mic',
    textIcon: 'DG',
    iconFile: '/providers/deepgram.png',
    category: 'audio',
    description: 'Deepgram speech-to-text transcription provider.',
    format: 'openai',
    authType: 'apikey',
    prefix: 'deepgram/',
    isBuiltIn: true,
    website: 'https://deepgram.com',
    color: '#13ef93',
    serviceKinds: ['stt'],
  },
  {
    id: 'opencode',
    displayName: 'OpenCode Free',
    icon: 'terminal',
    textIcon: 'OC',
    iconFile: '/providers/opencode.png',
    category: 'no-auth',
    description: 'Public OpenCode endpoint with no API key required.',
    format: 'openai',
    authType: 'none',
    prefix: 'oc/',
    isBuiltIn: true,
    website: 'https://opencode.ai',
    color: '#e87040',
    serviceKinds: ['llm'],
    hasFree: true,
    freeNote: 'No API key required. Public endpoint limits apply.',
  },
  {
    id: 'mimocode',
    displayName: 'MiMoCode Free',
    icon: 'devices',
    textIcon: 'MC',
    iconFile: '/providers/mimo.svg',
    category: 'no-auth',
    description: 'MiMoCode free tier with bootstrap JWT activation.',
    format: 'openai',
    authType: 'none',
    prefix: 'mimocode/',
    isBuiltIn: true,
    website: 'https://mimo.mi.com',
    color: '#ff6b35',
    serviceKinds: ['llm'],
    hasFree: true,
    freeNote: 'No API key required. Bootstrap tokens are generated automatically.',
  },
  {
    id: 'cf',
    displayName: 'Cloudflare Workers AI',
    icon: 'cloud',
    textIcon: 'CF',
    iconFile: '/providers/cloudflare.svg',
    category: 'apikey',
    description: 'Cloudflare Workers AI Gateway with OpenAI-compatible API. Supports @cf/ models.',
    format: 'openai',
    authType: 'custom',
    prefix: 'cf/',
    isBuiltIn: true,
    website: 'https://developers.cloudflare.com/ai-gateway/',
    color: '#F38020',
    serviceKinds: ['llm'],
    hasFree: true,
    freeNote: 'Workers AI free tier: 10,000 neurons/day per account.',
    inputFormat: 'pipe',
  },
];

const PROVIDER_ALIASES: Record<string, string> = {
  ag: 'antigravity',
  cx: 'codex',
  'mimocode-free': 'mimocode',
  'mimo-token': 'mimo-tp',
  'opencode-go': 'oc-go',
  'opencode-zen': 'oc-zen',
};

export function resolveProviderCatalogId(id: string): string {
  return PROVIDER_ALIASES[id] ?? id;
}

export function getCategoryById(id: string): ProviderCategory | undefined {
  return CATEGORIES.find((category) => category.id === id);
}

export function getProviderMeta(id: string): ProviderMeta | undefined {
  const catalogId = resolveProviderCatalogId(id);
  return PROVIDER_CATALOG.find((provider) => provider.id === catalogId);
}

export function getProviderIconSrc(id: string): string | null {
  return getProviderMeta(id)?.iconFile ?? null;
}

export function getCategoryForProvider(id: string): string {
  return getProviderMeta(id)?.category ?? 'compatible';
}

export function getCategoryColor(categoryId: string): string {
  return getCategoryById(categoryId)?.color ?? '#888888';
}

export function getStatusDotColor(status: string): string {
  switch (status) {
    case 'ready': return '#10b981';
    case 'rate_limited': return '#f59e0b';
    case 'quota_exhausted': return '#f97316';
    case 'balance_empty': return '#ef4444';
    case 'auth_failed': return '#ef4444';
    case 'suspended': return '#6b7280';
    case 'disabled': return '#6b7280';
    default: return '#6b7280';
  }
}

export function getStatusVariant(status: string): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status) {
    case 'ready': return 'default';
    case 'rate_limited': case 'quota_exhausted': return 'secondary';
    case 'balance_empty': case 'auth_failed': case 'suspended': return 'destructive';
    case 'disabled': return 'outline';
    default: return 'secondary';
  }
}

export function getStatusLabel(status: string): string {
  switch (status) {
    case 'ready': return 'Ready';
    case 'rate_limited': return 'Rate limited';
    case 'quota_exhausted': return 'Quota exhausted';
    case 'balance_empty': return 'Balance empty';
    case 'auth_failed': return 'Auth failed';
    case 'suspended': return 'Suspended';
    case 'disabled': return 'Disabled';
    default: return status;
  }
}
