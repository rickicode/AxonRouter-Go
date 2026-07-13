<script lang="ts">
 import * as Dialog from '$lib/components/ui/dialog';
 import { Button } from '$lib/components/ui/button';
 import { Input } from '$lib/components/ui/input';
 import { Label } from '$lib/components/ui/label';
 import { providersApi } from '$lib/api';
 import { toast } from 'svelte-sonner';

 let {
 open = $bindable(false),
 providerId,
 currentBaseUrl = '',
 currentDisplayName = '',
 onSaved,
 }: {
 open: boolean;
 providerId: string;
 currentBaseUrl?: string;
 currentDisplayName?: string;
 onSaved?: () => void;
 } = $props();

 let baseUrl = $state('');
 let displayName = $state('');
 let submitting = $state(false);

 $effect(() => {
 if (open) {
 baseUrl = currentBaseUrl ?? '';
 displayName = currentDisplayName ?? '';
 }
 });

 async function handleSave() {
 if (!baseUrl.trim()) {
 toast.error('Base URL is required');
 return;
 }
 submitting = true;
 try {
 await providersApi.update(providerId, {
 base_url: baseUrl.trim(),
 display_name: displayName.trim(),
 });
 toast.success('Provider updated');
 onSaved?.();
 open = false;
 } catch (err) {
 toast.error('Failed to update provider: ' + (err instanceof Error ? err.message : 'Unknown'));
 } finally {
 submitting = false;
 }
 }
</script>

<Dialog.Root bind:open>
 <Dialog.Content class="sm:max-w-md">
 <Dialog.Header>
 <Dialog.Title class="text-display-md">Edit provider.</Dialog.Title>
 <Dialog.Description class="text-body-sm text-muted-foreground">
 Update the base URL and display name for this custom provider.
 </Dialog.Description>
 </Dialog.Header>

 <div class="space-y-4 py-2">
 <div class="space-y-2">
 <Label class="text-body-sm">Display name</Label>
 <Input bind:value={displayName} placeholder="Display name" class="h-9 text-body-sm rounded-sm" />
 </div>
 <div class="space-y-2">
 <Label class="text-body-sm">Base URL</Label>
 <Input bind:value={baseUrl} placeholder="https://..." class="h-9 text-body-sm rounded-sm font-mono" />
 <p class="text-caption text-muted-foreground">The OpenAI-compatible endpoint this provider proxies to (e.g. https://api.example.com/v1).</p>
 </div>
 </div>

 <Dialog.Footer>
 <Button variant="outline" onclick={() => (open = false)} class="text-body-sm rounded-sm">Cancel</Button>
 <Button onclick={handleSave} disabled={submitting} class="text-body-sm rounded-sm">
 {submitting ? 'Saving...' : 'Save'}
 </Button>
 </Dialog.Footer>
 </Dialog.Content>
</Dialog.Root>
