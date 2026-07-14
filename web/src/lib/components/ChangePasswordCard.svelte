<script lang="ts">
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import { passwordApi } from '$lib/api';
import { setMustChangePassword } from '$lib/auth';
import { toast } from 'svelte-sonner';
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
		toast.error('Current password is required');
		return;
	}
	if (!newPassword || newPassword.length < 8) {
		toast.error('New password must be at least 8 characters');
		return;
	}
	if (newPassword !== confirmPassword) {
		toast.error('New passwords do not match');
		return;
	}
	loading = true;
	try {
		await passwordApi.change(currentPassword, newPassword, confirmPassword);
		setMustChangePassword(false);
		currentPassword = '';
		newPassword = '';
		confirmPassword = '';
		toast.success('Password updated');
	} catch (err) {
		toast.error(err instanceof Error ? err.message : 'Failed to update password');
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
				<CardTitle class="text-body-md-strong">Change Password</CardTitle>
				<CardDescription class="text-body-sm">Update the admin dashboard password.</CardDescription>
			</div>
		</div>
	</CardHeader>
<CardContent>
  <form class="space-y-4 max-w-xl" onsubmit={submit}>
    <div class="space-y-2">
      <Label for="current-password" class="text-body-sm-strong">Current Password</Label>
      <div class="relative">
        <Input
          id="current-password"
          type={showCurrent ? 'text' : 'password'}
          placeholder="Enter current password"
          autocomplete="current-password"
          class="h-11 pr-10"
          bind:value={currentPassword}
        />
        <button
          type="button"
          class="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          onclick={() => toggle('current')}
          aria-label={showCurrent ? 'Hide password' : 'Show password'}
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
      <Label for="new-password" class="text-body-sm-strong">New Password</Label>
      <div class="relative">
        <Input
          id="new-password"
          type={showNew ? 'text' : 'password'}
          placeholder="Enter new password (min. 8 characters)"
          autocomplete="new-password"
          class="h-11 pr-10"
          bind:value={newPassword}
        />
        <button
          type="button"
          class="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          onclick={() => toggle('new')}
          aria-label={showNew ? 'Hide password' : 'Show password'}
        >
          {#if showNew}
            <EyeOffIcon class="size-4" />
          {:else}
            <EyeIcon class="size-4" />
          {/if}
        </button>
      </div>
      <p class="text-caption text-muted-foreground">Use at least 8 characters.</p>
    </div>

    <div class="space-y-2">
      <Label for="confirm-password" class="text-body-sm-strong">Confirm New Password</Label>
      <div class="relative">
        <Input
          id="confirm-password"
          type={showConfirm ? 'text' : 'password'}
          placeholder="Repeat new password"
          autocomplete="new-password"
          class="h-11 pr-10"
          bind:value={confirmPassword}
        />
        <button
          type="button"
          class="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          onclick={() => toggle('confirm')}
          aria-label={showConfirm ? 'Hide password' : 'Show password'}
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
          <span>Saving…</span>
        {:else}
          <LockIcon class="size-4 mr-2" />
          <span>Update Password</span>
        {/if}
      </Button>
    </div>
  </form>
</CardContent>
</Card>
