<script lang="ts">
import * as Dialog from '$lib/components/ui/dialog';
import { Button } from '$lib/components/ui/button';
import { setMustChangePassword, dismissPasswordWarning } from '$lib/auth';
import { router } from '$lib/router';
import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';

let open = $state(true);

$effect(() => {
	if (!open) {
		setMustChangePassword(false);
		dismissPasswordWarning();
	}
});

function goToSettings() {
	router.navigate('/settings');
	open = false;
}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-[420px]">
		<Dialog.Header>
			<div class="flex items-center gap-3">
				<span class="flex size-10 items-center justify-center rounded-full bg-destructive/10 text-destructive">
					<ShieldAlertIcon class="size-5" />
				</span>
				<div>
					<Dialog.Title class="text-display-md">Security Warning</Dialog.Title>
					<Dialog.Description class="text-body-sm text-muted-foreground">
						The admin password is still the default or has not been changed.
					</Dialog.Description>
				</div>
			</div>
		</Dialog.Header>

		<div class="py-2 text-body-sm text-muted-foreground space-y-2">
			<p>
				For dashboard security, update the password in Settings. No API access is blocked — this is only a warning.
			</p>
		</div>

		<Dialog.Footer class="flex-col gap-2 sm:flex-col">
			<Button onclick={goToSettings} class="h-11 w-full">
				Update Password
			</Button>
			<Button
				type="button"
				variant="outline"
				class="h-11 w-full"
				onclick={() => (open = false)}
			>
				Close Warning
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
