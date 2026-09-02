<script lang="ts">
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import { passwordApi } from '$lib/api';
import { setMustChangePassword } from '$lib/auth';
import { toast } from 'svelte-sonner';
import { t, getT } from '$lib/i18n';
import LockIcon from '@lucide/svelte/icons/lock';
import EyeIcon from '@lucide/svelte/icons/eye';
import EyeOffIcon from '@lucide/svelte/icons/eye-off';
import Loader2Icon from '@lucide/svelte/icons/loader-2';

let currentPassword = $state('');
let newPassword = $state('');
let confirmPassword = $state('');
let showCurrent = $state(false);
let showNew = $state(false);
let showConfirm = $state(false);
let loading = $state(false);

function toggle(field: 'current' | 'new' | 'confirm') {
	if (field === 'current') showCurrent = !showCurrent;
	else if (field === 'new') showNew = !showNew;
	else showConfirm = !showConfirm;
}

async function submit(event: SubmitEvent) {
	event.preventDefault();
	if (!currentPassword) {
		toast.error(getT()('changePassword.requiredCurrent'));
		return;
	}
	if (!newPassword || newPassword.length < 8) {
		toast.error(getT()('changePassword.minLengthNew'));
		return;
	}
	if (newPassword !== confirmPassword) {
		toast.error(getT()('changePassword.mismatch'));
		return;
	}
	loading = true;
	try {
		await passwordApi.change(currentPassword, newPassword, confirmPassword);
		setMustChangePassword(false);
		currentPassword = '';
		newPassword = '';
		confirmPassword = '';
		toast.success(getT()('changePassword.updated'));
	} catch (err) {
		toast.error(err instanceof Error ? err.message : getT()('changePassword.failed'));
	} finally {
		loading = false;
	}
}
</script>

<Card class="shadow-card border-border/60">
	<CardHeader class="pb-4">
		<div class="flex items-center gap-3">
			<span class="flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
				<LockIcon class="size-5" />
			</span>
			<div>
				<CardTitle class="text-body-md-strong">{$t('changePassword.title')}</CardTitle>
				<CardDescription class="text-body-sm">{$t('changePassword.subtitle')}</CardDescription>
			</div>
		</div>
	</CardHeader>
<CardContent>
  <form class="space-y-4 max-w-xl" onsubmit={submit}>
    <div class="space-y-2">
      <Label for="current-password" class="text-body-sm-strong">{$t('changePassword.currentLabel')}</Label>
      <div class="relative">
        <Input
          id="current-password"
          type={showCurrent ? 'text' : 'password'}
          placeholder="{$t('changePassword.currentPlaceholder')}"
          autocomplete="current-password"
          class="h-11 pr-10"
          bind:value={currentPassword}
        />
        <button
          type="button"
          class="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          onclick={() => toggle('current')}
          aria-label={showCurrent ? $t('changePassword.hidePassword') : $t('changePassword.showPassword')}
        >
          {#if showCurrent}
            <EyeOffIcon class="size-4" />
          {:else}
            <EyeIcon class="size-4" />
          {/if}
        </button>
      </div>
    </div>

    <div class="space-y-2">
      <Label for="new-password" class="text-body-sm-strong">{$t('changePassword.newLabel')}</Label>
      <div class="relative">
        <Input
          id="new-password"
          type={showNew ? 'text' : 'password'}
          placeholder="{$t('changePassword.newPlaceholder')}"
          autocomplete="new-password"
          class="h-11 pr-10"
          bind:value={newPassword}
        />
        <button
          type="button"
          class="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          onclick={() => toggle('new')}
          aria-label={showNew ? $t('changePassword.hidePassword') : $t('changePassword.showPassword')}
        >
          {#if showNew}
            <EyeOffIcon class="size-4" />
          {:else}
            <EyeIcon class="size-4" />
          {/if}
        </button>
      </div>
      <p class="text-caption text-muted-foreground">{$t('changePassword.minLengthHint')}</p>
    </div>

    <div class="space-y-2">
      <Label for="confirm-password" class="text-body-sm-strong">{$t('changePassword.confirmLabel')}</Label>
      <div class="relative">
        <Input
          id="confirm-password"
          type={showConfirm ? 'text' : 'password'}
          placeholder="{$t('changePassword.confirmPlaceholder')}"
          autocomplete="new-password"
          class="h-11 pr-10"
          bind:value={confirmPassword}
        />
        <button
          type="button"
          class="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          onclick={() => toggle('confirm')}
          aria-label={showConfirm ? $t('changePassword.hidePassword') : $t('changePassword.showPassword')}
        >
          {#if showConfirm}
            <EyeOffIcon class="size-4" />
          {:else}
            <EyeIcon class="size-4" />
          {/if}
        </button>
      </div>
    </div>

    <div class="pt-1">
      <Button type="submit" class="h-11" disabled={loading}>
        {#if loading}
          <Loader2Icon class="size-4 animate-spin mr-2" />
          <span>{$t('changePassword.saving')}</span>
        {:else}
          <LockIcon class="size-4 mr-2" />
          <span>{$t('changePassword.updateButton')}</span>
        {/if}
      </Button>
    </div>
  </form>
</CardContent>
</Card>
