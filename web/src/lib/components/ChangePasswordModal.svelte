<script lang="ts">
import * as Dialog from '$lib/components/ui/dialog';
import { Button } from '$lib/components/ui/button';
import { setMustChangePassword } from '$lib/auth';
import { router } from '$lib/router';
import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';

let open = $state(true);

$effect(() => {
	if (!open) {
		setMustChangePassword(false);
	}
});

function goToSettings() {
	open = false;
	setMustChangePassword(false);
	router.navigate('/settings');
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
					<Dialog.Title class="text-display-md">Peringatan Keamanan</Dialog.Title>
					<Dialog.Description class="text-body-sm text-muted-foreground">
						Password administrator masih default atau belum diubah.
					</Dialog.Description>
				</div>
			</div>
		</Dialog.Header>

		<div class="py-2 text-body-sm text-muted-foreground space-y-2">
			<p>
				Demi keamanan dashboard, ubah password default melalui halaman Pengaturan.
				Tidak ada akses API yang diblokir, hanya peringatan ini.
			</p>
		</div>

		<Dialog.Footer class="flex-col gap-2 sm:flex-col">
			<Button onclick={goToSettings} class="h-11 w-full">
				Ya, Ubah Password
			</Button>
			<Button
				type="button"
				variant="outline"
				class="h-11 w-full"
				onclick={() => (open = false)}
			>
				Tutup Peringatan
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
