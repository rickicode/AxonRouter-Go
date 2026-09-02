<script lang="ts">
  import { t } from '$lib/i18n';
  import { locale, availableLocales, setLocale } from '$lib/i18n';
  import * as Select from '$lib/components/ui/select';
  import LanguagesIcon from '@lucide/svelte/icons/languages';

  let { variant = 'header' }: { variant?: 'header' | 'login' } = $props();

  // Current locale code as a plain string for the Select value.
  let current = $state($locale);
  // Keep in sync if another component switches locale.
  $effect(() => {
    current = $locale;
  });

  async function onChange(value: string) {
    const match = availableLocales.find((l) => l.code === value);
    if (!match) return;
    await setLocale(match.code);
  }

  function currentName(): string {
    const match = availableLocales.find((l) => l.code === current);
    if (!match) return 'English';
    return match.name;
  }
</script>

{#if variant === 'header'}
  <Select.Root type="single" value={current} onValueChange={onChange}>
    <Select.Trigger
      class="flex h-8 items-center gap-1.5 rounded-md border border-border bg-background px-2 text-caption-mono text-muted-foreground transition-colors hover:text-foreground cursor-pointer"
      aria-label="Language"
    >
      <LanguagesIcon class="size-3.5" />
      <span class="max-w-24 truncate">{currentName()}</span>
    </Select.Trigger>
    <Select.Content class="max-h-72">
      {#each availableLocales as l (l.code)}
        <Select.Item value={l.code} class="text-body-sm">
          <span class="flex items-center gap-2">
            <span>{l.name}</span>
            {#if !l.translated}
              <span class="text-caption text-muted-foreground/60" title={$t('language.untranslatedHint')}>(English)</span>
            {/if}
          </span>
        </Select.Item>
      {/each}
    </Select.Content>
  </Select.Root>
{:else}
  <Select.Root type="single" value={current} onValueChange={onChange}>
    <Select.Trigger
      class="mx-auto mt-5 flex h-8 items-center gap-1.5 rounded-md border border-border bg-background/60 px-3 text-caption text-muted-foreground transition-colors hover:text-foreground cursor-pointer"
      aria-label="Language"
    >
      <LanguagesIcon class="size-3.5" />
      <span>{currentName()}</span>
    </Select.Trigger>
    <Select.Content class="max-h-72">
      {#each availableLocales as l (l.code)}
        <Select.Item value={l.code} class="text-body-sm">
          <span class="flex items-center gap-2">
            <span>{l.name}</span>
            {#if !l.translated}
              <span class="text-caption text-muted-foreground/60" title={$t('language.untranslatedHint')}>(English)</span>
            {/if}
          </span>
        </Select.Item>
      {/each}
    </Select.Content>
  </Select.Root>
{/if}