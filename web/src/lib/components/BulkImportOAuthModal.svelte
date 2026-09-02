<script lang="ts">
  import * as Dialog from '$lib/components/ui/dialog';
  import { Button } from '$lib/components/ui/button';
  import { Label } from '$lib/components/ui/label';
  import { Textarea } from '$lib/components/ui/textarea';
  import { oauthApi } from '$lib/api';
  import { toast } from 'svelte-sonner';

  type Account = Record<string, unknown>;
  type Result = { index: number; ok: boolean; id?: string; error?: string };

  let {
    open = $bindable(false),
    onCreated,
    provider = 'codex',
    providerLabel = 'Codex',
  }: {
    open: boolean;
    onCreated?: () => void;
    provider?: 'codex' | 'grok-cli';
    providerLabel?: string;
  } = $props();

  let jsonText = $state('');
  let submitting = $state(false);
  let parseError = $state('');
  let isDragging = $state(false);
  let fileSummary = $state<{ files: number; accounts: number } | null>(null);
  let result = $state<{ success: number; failed: number; results: Result[] } | null>(null);

  const placeholder = $derived(provider === 'grok-cli' ? `[
  {
    "access_token": "eyJ0eXAiOiJhdCtqd3Qi...",
    "refresh_token": "LZhriF9bf88pPykpXCuZ9...",
    "id_token": "eyJ0eXAiOiJKV1QiLCJhbGci...",
    "email": "account@example.com"
  }
]` : `[
  {
    "accessToken": "eyJhbGc...",
    "refreshToken": "rt_...",
    "idToken": "eyJhbGc...",
    "email": "user@example.com"
  }
]`);

  function isAccount(value: unknown): value is Account {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
  }

  function normalizeAccounts(value: unknown): Account[] | null {
    if (Array.isArray(value)) return value.filter(isAccount);
    if (isAccount(value) && Array.isArray(value.accounts)) return value.accounts.filter(isAccount);
    return isAccount(value) ? [value] : null;
  }

  function parseAccountsText(text: string): Account[] | null {
    const trimmed = text.trim();
    if (!trimmed) return null;

    let parsed: unknown;
    try {
      parsed = JSON.parse(trimmed);
    } catch (initialError) {
      if (provider !== 'grok-cli') throw initialError;
      let fixed = trimmed;
      if (!fixed.startsWith('[')) {
        fixed = fixed.replace(/}\s*,\s*{/g, '},{').replace(/}\s*{/g, '},{');
        if (fixed.endsWith(',')) fixed = fixed.slice(0, -1);
        fixed = `[${fixed}]`;
      }
      parsed = JSON.parse(fixed);
    }
    return normalizeAccounts(parsed);
  }

  function parseInput(): { accounts: Account[] | null; error?: string } {
    try {
      const accounts = parseAccountsText(jsonText);
      return accounts && accounts.length > 0
        ? { accounts }
        : { accounts: null, error: 'No account objects found in input' };
    } catch (err) {
      return { accounts: null, error: `Invalid JSON: ${err instanceof Error ? err.message : 'parse failed'}` };
    }
  }

  function accountCount(): number {
    if (!jsonText.trim()) return 0;
    return parseInput().accounts?.length ?? 0;
  }

  function reset() {
    jsonText = '';
    parseError = '';
    result = null;
    fileSummary = null;
    isDragging = false;
  }

  function handleClose() {
    if (submitting) return;
    reset();
    open = false;
  }

  async function processFiles(files: FileList | File[]) {
    const jsonFiles = Array.from(files).filter((file) =>
      file.name.endsWith('.json') || file.type === 'application/json' || file.type === ''
    );
    if (jsonFiles.length === 0) {
      parseError = 'Please select valid .json files';
      return;
    }

    try {
      const accounts: Account[] = [];
      for (const file of jsonFiles) {
        const parsed = parseAccountsText(await file.text());
        if (parsed) accounts.push(...parsed);
      }
      if (accounts.length === 0) {
        parseError = 'No accounts found in selected files';
        return;
      }
      jsonText = JSON.stringify(accounts, null, 2);
      fileSummary = { files: jsonFiles.length, accounts: accounts.length };
      parseError = '';
    } catch (err) {
      parseError = `Error reading files: ${err instanceof Error ? err.message : 'invalid JSON'}`;
    }
  }

  function handleFileChange(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    if (input.files) void processFiles(input.files);
    input.value = '';
  }

  function handleDrop(event: DragEvent) {
    event.preventDefault();
    isDragging = false;
    if (event.dataTransfer?.files) void processFiles(event.dataTransfer.files);
  }

  async function handleSubmit() {
    parseError = '';
    result = null;
    const parsed = parseInput();
    if (!parsed.accounts) {
      parseError = parsed.error ?? 'No account objects found in input';
      toast.error(parseError);
      return;
    }

    submitting = true;
    try {
      const response = provider === 'grok-cli'
        ? await oauthApi.bulkImportGrokCli(parsed.accounts)
        : await oauthApi.bulkImportCodex(parsed.accounts);
      result = response;
      if (response.failed > 0) {
        toast.error(`Imported ${response.success}, ${response.failed} failed`);
      } else {
        toast.success(`Imported ${response.success} ${providerLabel} account${response.success === 1 ? '' : 's'}`);
      }
      if (response.success > 0) onCreated?.();
    } catch (err) {
      parseError = err instanceof Error ? err.message : 'Bulk import failed';
      toast.error(parseError);
    } finally {
      submitting = false;
    }
  }

  const failedItems = $derived(result?.results.filter((item) => !item.ok) ?? []);
</script>

<Dialog.Root bind:open onOpenChange={(value) => { if (!value) handleClose(); }}>
  <Dialog.Content class="sm:max-w-2xl">
    <Dialog.Header>
      <Dialog.Title class="text-lg font-semibold">Bulk add {providerLabel} accounts</Dialog.Title>
      <Dialog.Description class="text-sm text-muted-foreground">
        {provider === 'grok-cli' ? 'Upload JSON files or paste a JSON array / object.' : 'Paste OAuth account objects. Each account needs an access token.'}
      </Dialog.Description>
    </Dialog.Header>

    <div class="flex flex-col gap-3">
      <div class="flex flex-col gap-1.5">
        <Label for="oauth-bulk-json" class="text-sm font-medium">Account JSON</Label>
        <div class="flex items-center justify-between gap-2">
          <p class="text-caption text-muted-foreground">Upload multiple .json files or paste JSON.</p>
          <label class="inline-flex cursor-pointer items-center gap-1.5 rounded-sm border border-border px-2.5 py-1.5 text-caption text-muted-foreground transition-colors hover:border-primary/50 hover:text-foreground">
            <span>Upload JSON</span>
            <input type="file" accept=".json,application/json" multiple class="hidden" onchange={handleFileChange} disabled={submitting} />
          </label>
        </div>
        <div
          class="relative"
          ondragover={(event) => { event.preventDefault(); isDragging = true; }}
          ondragleave={() => isDragging = false}
          ondrop={handleDrop}
        >
          <Textarea
            id="oauth-bulk-json"
            bind:value={jsonText}
            placeholder={placeholder}
            class="min-h-60 font-mono text-xs"
            spellcheck={false}
            disabled={submitting}
          />
          {#if isDragging}
            <div class="pointer-events-none absolute inset-0 flex items-center justify-center rounded-md border-2 border-dashed border-primary bg-background/90 text-body-sm text-primary">
              Drop .json files here
            </div>
          {/if}
        </div>
        {#if fileSummary}
          <p class="text-caption text-emerald-400">Loaded {fileSummary.accounts} account{fileSummary.accounts === 1 ? '' : 's'} from {fileSummary.files} file{fileSummary.files === 1 ? '' : 's'}</p>
        {/if}
        {#if jsonText.trim()}
          <p class="text-caption text-emerald-400">{accountCount()} account{accountCount() === 1 ? '' : 's'} detected</p>
        {/if}
        <p class="text-caption text-muted-foreground">
          Accepts a JSON array, one account object, or <span class="font-mono">{"{ accounts: [...] }"}</span>. Tokens are not returned in the result.
        </p>
      </div>

      {#if parseError}
        <p class="break-words rounded-md border border-destructive/20 bg-destructive/5 px-3 py-2 text-body-sm text-destructive">{parseError}</p>
      {/if}

      {#if result}
        <div class="flex flex-col gap-2 rounded-md border border-border bg-muted/20 p-3">
          <p class="text-sm font-medium {result.failed > 0 ? 'text-amber-400' : 'text-emerald-400'}">
            {result.success} added{result.failed > 0 ? `, ${result.failed} failed` : ''}
          </p>
          {#if failedItems.length > 0}
            <div class="max-h-32 overflow-y-auto rounded border border-destructive/20 bg-destructive/5 p-2 text-xs font-mono text-destructive">
              {#each failedItems as item}
                <div>[{item.index}] {item.error ?? 'Import failed'}</div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </div>

    <Dialog.Footer>
      <Button variant="outline" onclick={handleClose} disabled={submitting} class="text-body-sm rounded-sm">Close</Button>
      <Button onclick={handleSubmit} disabled={submitting || !jsonText.trim()} class="text-body-sm rounded-sm">
        {submitting ? 'Importing…' : 'Import all'}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
