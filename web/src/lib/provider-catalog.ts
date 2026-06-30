// Static provider catalog — defines categories, icons, display metadata
// Merged with runtime connection data from API
// Source of truth: docs/PRD.md Section 4.2

export interface ProviderCategory {
  id: string;
  label: string;
  color: string;
}

export interface ProviderMeta {
  id: string;
  displayName: string;
  icon: string;        // emoji fallback
  iconFile?: string;   // /providers/xxx.png|svg path — preferred over emoji
  category: string;
  description: string;
  format: string;
  authType: 'none' | 'apikey' | 'oauth' | 'custom';
  prefix: string;
  isBuiltIn: boolean;
  website?: string;
  color: string;
}
export const CATEGORIES: ProviderCategory[] = [
  { id: 'oauth', label: 'OAuth', color: '#0070f3' },
  { id: 'apikey', label: 'API Key', color: '#a855f7' },
  { id: 'free', label: 'Free', color: '#10b981' },
  { id: 'free_tier', label: 'Free Tier', color: '#34d399' },
  { id: 'compatible', label: 'Compatible', color: '#06b6d4' },
  { id: 'aggregator', label: 'Aggregator', color: '#f59e0b' },
  { id: 'ide', label: 'IDE', color: '#6366f1' },
];

export const PROVIDER_CATALOG: ProviderMeta[] = [
  // ─── OAuth Providers ───
  { id: 'codex', displayName: 'Codex', icon: '🔮', iconFile: '/providers/codex.svg', category: 'oauth', description: 'OpenAI Codex via OAuth device code flow', format: 'openai-responses', authType: 'oauth', prefix: 'cx/', isBuiltIn: true, website: 'https://openai.com', color: '#10a37f' },
  { id: 'antigravity', displayName: 'Antigravity', icon: '🪐', iconFile: '/providers/antigravity.png', category: 'oauth', description: 'Antigravity AI via Google OAuth', format: 'antigravity', authType: 'oauth', prefix: 'ag/', isBuiltIn: true, color: '#4285f4' },
  { id: 'kiro', displayName: 'Kiro', icon: '⚡', iconFile: '/providers/kiro.svg', category: 'oauth', description: 'Kiro AI via AWS OAuth', format: 'kiro', authType: 'oauth', prefix: 'kiro/', isBuiltIn: true, color: '#ff9900' },
  { id: 'claude', displayName: 'Claude', icon: '🧠', iconFile: '/providers/claude.svg', category: 'oauth', description: 'Anthropic Claude via OAuth PKCE', format: 'claude', authType: 'oauth', prefix: 'claude/', isBuiltIn: true, website: 'https://anthropic.com', color: '#d4a574' },

  // ─── API Key Providers ───
  { id: 'openai', displayName: 'OpenAI', icon: '🤖', iconFile: '/providers/openai.png', category: 'apikey', description: 'OpenAI GPT models', format: 'openai', authType: 'apikey', prefix: 'openai/', isBuiltIn: true, website: 'https://openai.com', color: '#10a37f' },
  { id: 'gemini', displayName: 'Gemini', icon: '💎', iconFile: '/providers/gemini-cli.svg', category: 'apikey', description: 'Google Gemini models', format: 'gemini', authType: 'apikey', prefix: 'gemini/', isBuiltIn: true, website: 'https://ai.google.dev', color: '#4285f4' },
  { id: 'deepseek', displayName: 'DeepSeek', icon: '🔍', iconFile: '/providers/deepseek.png', category: 'apikey', description: 'DeepSeek AI models', format: 'openai', authType: 'apikey', prefix: 'deepseek/', isBuiltIn: true, website: 'https://deepseek.com', color: '#0066ff' },
  { id: 'groq', displayName: 'Groq', icon: '⚡', iconFile: '/providers/groq.png', category: 'apikey', description: 'Groq fast inference (LPU)', format: 'openai', authType: 'apikey', prefix: 'groq/', isBuiltIn: true, website: 'https://groq.com', color: '#f55036' },
  { id: 'elevenlabs', displayName: 'ElevenLabs', icon: '🔊', iconFile: '/providers/elevenlabs.png', category: 'apikey', description: 'ElevenLabs TTS synthesis', format: 'openai', authType: 'apikey', prefix: 'elevenlabs/', isBuiltIn: true, website: 'https://elevenlabs.io', color: '#000000' },
  { id: 'deepgram', displayName: 'Deepgram', icon: '🎙️', iconFile: '/providers/deepgram.png', category: 'apikey', description: 'Deepgram STT transcription', format: 'openai', authType: 'apikey', prefix: 'deepgram/', isBuiltIn: true, website: 'https://deepgram.com', color: '#13ef96' },
  { id: 'mimo', displayName: 'MiMo PAYG', icon: '🎯', iconFile: '/providers/mimo.png', category: 'apikey', description: 'Xiaomi MiMo pay-as-you-go', format: 'openai', authType: 'apikey', prefix: 'mimo/', isBuiltIn: true, website: 'https://mimo.xiaomi.com', color: '#ff6900' },
  { id: 'mimo-tp', displayName: 'MiMo Token Plan', icon: '🎯', iconFile: '/providers/mimo.png', category: 'apikey', description: 'MiMo monthly token plan (4.1B tokens/mo)', format: 'openai', authType: 'apikey', prefix: 'mimo-tp/', isBuiltIn: true, color: '#ff6900' },
  { id: 'oc-zen', displayName: 'OpenCode Zen', icon: '📝', iconFile: '/providers/opencode-zen.png', category: 'apikey', description: 'OpenCode Zen tier with API key', format: 'openai', authType: 'apikey', prefix: 'oc-zen/', isBuiltIn: true, website: 'https://opencode.ai/zen', color: '#6366f1' },
  { id: 'oc-go', displayName: 'OpenCode Go', icon: '🚀', iconFile: '/providers/opencode.png', category: 'apikey', description: 'OpenCode Go tier (Qwen models)', format: 'openai', authType: 'apikey', prefix: 'oc-go/', isBuiltIn: true, website: 'https://opencode.ai/go', color: '#22c55e' },

  // ─── Free Providers ───
  { id: 'opencode', displayName: 'OpenCode Free', icon: '📝', iconFile: '/providers/opencode.svg', category: 'free', description: 'OpenCode free tier — Kimi, GLM, Qwen, MiMo, MiniMax', format: 'openai', authType: 'none', prefix: 'oc/', isBuiltIn: true, website: 'https://opencode.ai', color: '#6366f1' },
  { id: 'mimocode', displayName: 'MiMoCode Free', icon: '🎯', iconFile: '/providers/mimo.png', category: 'free', description: 'MiMoCode free tier — auto-activated bootstrap JWT', format: 'openai', authType: 'none', prefix: 'mimocode/', isBuiltIn: true, color: '#ff6900' },
];

/** Get category metadata by id */
export function getCategoryById(id: string): ProviderCategory | undefined {
  return CATEGORIES.find(c => c.id === id);
}

/** Get provider metadata by id */
export function getProviderMeta(id: string): ProviderMeta | undefined {
  return PROVIDER_CATALOG.find(p => p.id === id);
}

/** Get the best icon source for a provider — iconFile if available, else null */
export function getProviderIconSrc(id: string): string | null {
  return getProviderMeta(id)?.iconFile ?? null;
}

/** Get category string for a provider id */
export function getCategoryForProvider(id: string): string {
  return getProviderMeta(id)?.category ?? 'compatible';
}

/** Get category color as a CSS color */
export function getCategoryColor(categoryId: string): string {
  return getCategoryById(categoryId)?.color ?? '#888888';
}

/** Status dot color for connection status */
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

/** Status badge variant for shadcn Badge component */
export function getStatusVariant(status: string): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status) {
    case 'ready': return 'default';
    case 'rate_limited': case 'quota_exhausted': return 'secondary';
    case 'balance_empty': case 'auth_failed': case 'suspended': return 'destructive';
    case 'disabled': return 'outline';
    default: return 'secondary';
  }
}

/** Human-readable status label */
export function getStatusLabel(status: string): string {
  switch (status) {
    case 'ready': return 'Ready';
    case 'rate_limited': return 'Rate Limited';
    case 'quota_exhausted': return 'Quota Exhausted';
    case 'balance_empty': return 'Balance Empty';
    case 'auth_failed': return 'Auth Failed';
    case 'suspended': return 'Suspended';
    case 'disabled': return 'Disabled';
    default: return status;
  }
}
