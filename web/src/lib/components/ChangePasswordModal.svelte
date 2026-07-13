<script lang="ts">
import * as Dialog from '$lib/components/ui/dialog';
import * as AlertDialog from '$lib/components/ui/alert-dialog';
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import { passwordApi } from '$lib/api';
import { setMustChangePassword } from '$lib/auth';
import { toast } from 'svelte-sonner';
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
let error = $state('');
let showDeferDialog = $state(false);

function reset() {
  currentPassword = '';
  newPassword = '';
  confirmPassword = '';
  showCurrent = false;
  showNew = false;
  showConfirm = false;
  error = '';
}

async function handleSubmit(event: SubmitEvent) {
  event.preventDefault();
  error = '';

  if (!currentPassword) {
    error = 'Password saat ini wajib diisi';
    return;
  }
  if (!newPassword) {
    error = 'Password baru wajib diisi';
    return;
  }
  if (newPassword !== confirmPassword) {
    error = 'Konfirmasi password baru tidak cocok';
    return;
  }

  loading = true;
  try {
    await passwordApi.change(currentPassword, newPassword, confirmPassword);
    setMustChangePassword(false);
    reset();
    toast.success('Password berhasil diubah');
  } catch (e) {
    error = e instanceof Error ? e.message : 'Gagal mengubah password';
    toast.error(error);
  } finally {
    loading = false;
  }
}

async function handleDeferConfirm() {
  loading = true;
  try {
    await passwordApi.deferChange();
    setMustChangePassword(false);
    showDeferDialog = false;
    reset();
    toast.info('Pengubahan password ditunda selama 24 jam');
  } catch (e) {
    error = e instanceof Error ? e.message : 'Gagal menunda pengubahan password';
    showDeferDialog = false;
    toast.error(error);
  } finally {
    loading = false;
  }
}

function openDeferDialog() {
  showDeferDialog = true;
}

function toggleShow(field: 'current' | 'new' | 'confirm') {
  if (field === 'current') showCurrent = !showCurrent;
  else if (field === 'new') showNew = !showNew;
  else showConfirm = !showConfirm;
}
</script>

<Dialog.Root open={true}>
  <Dialog.Content
    class="sm:max-w-[420px]"
    showCloseButton={false}
    interactOutsideBehavior="ignore"
    escapeKeydownBehavior="ignore"
  >
    <Dialog.Header>
      <Dialog.Title class="text-display-md">Ubah Password</Dialog.Title>
      <Dialog.Description class="text-body-sm text-muted-foreground">
        Ubah password default Anda untuk menjaga keamanan dashboard.
      </Dialog.Description>
    </Dialog.Header>

    <form class="flex flex-col gap-4 py-2" onsubmit={handleSubmit}>
      <div class="flex flex-col gap-2">
        <Label for="current-password" class="text-body-sm-strong">Password Saat Ini</Label>
        <div class="relative">
          <Input
            id="current-password"
            type={showCurrent ? 'text' : 'password'}
            placeholder="Masukkan password saat ini"
            autocomplete="current-password"
            class="h-11 pr-10"
            bind:value={currentPassword}
          />
          <button
            type="button"
            class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground transition-colors hover:text-foreground"
            onclick={() => toggleShow('current')}
            aria-label={showCurrent ? 'Sembunyikan password' : 'Tampilkan password'}
          >
            {#if showCurrent}
              <EyeOffIcon class="size-4" />
            {:else}
              <EyeIcon class="size-4" />
            {/if}
          </button>
        </div>
      </div>

      <div class="flex flex-col gap-2">
        <Label for="new-password" class="text-body-sm-strong">Password Baru</Label>
        <div class="relative">
          <Input
            id="new-password"
            type={showNew ? 'text' : 'password'}
            placeholder="Masukkan password baru"
            autocomplete="new-password"
            class="h-11 pr-10"
            bind:value={newPassword}
          />
          <button
            type="button"
            class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground transition-colors hover:text-foreground"
            onclick={() => toggleShow('new')}
            aria-label={showNew ? 'Sembunyikan password' : 'Tampilkan password'}
          >
            {#if showNew}
              <EyeOffIcon class="size-4" />
            {:else}
              <EyeIcon class="size-4" />
            {/if}
          </button>
        </div>
      </div>

      <div class="flex flex-col gap-2">
        <Label for="confirm-password" class="text-body-sm-strong">Konfirmasi Password Baru</Label>
        <div class="relative">
          <Input
            id="confirm-password"
            type={showConfirm ? 'text' : 'password'}
            placeholder="Ulangi password baru"
            autocomplete="new-password"
            class="h-11 pr-10"
            bind:value={confirmPassword}
          />
          <button
            type="button"
            class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground transition-colors hover:text-foreground"
            onclick={() => toggleShow('confirm')}
            aria-label={showConfirm ? 'Sembunyikan password' : 'Tampilkan password'}
          >
            {#if showConfirm}
              <EyeOffIcon class="size-4" />
            {:else}
              <EyeIcon class="size-4" />
            {/if}
          </button>
        </div>
      </div>

      {#if error}
        <p class="text-caption text-destructive">{error}</p>
      {/if}

      <Dialog.Footer class="flex-col gap-2 sm:flex-col">
        <Button type="submit" class="h-11 w-full gap-2" disabled={loading}>
          {#if loading}
            <Loader2Icon class="size-4 animate-spin" />
            <span>Menyimpan…</span>
          {:else}
            <span>Simpan</span>
          {/if}
        </Button>
        <Button
          type="button"
          variant="outline"
          class="h-11 w-full"
          disabled={loading}
          onclick={openDeferDialog}
        >
          Lakukan Nanti
        </Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>

<AlertDialog.Root bind:open={showDeferDialog}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title class="text-display-md">Peringatan Keamanan</AlertDialog.Title>
      <AlertDialog.Description class="text-body-sm text-muted-foreground">
        Menunda pengubahan password meningkatkan risiko akses tidak sah. Anda akan diminta lagi dalam 24 jam.
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel disabled={loading}>Batal</AlertDialog.Cancel>
      <AlertDialog.Action disabled={loading} onclick={handleDeferConfirm}>
        {#if loading}
          <Loader2Icon class="size-4 animate-spin" />
        {:else}
          Konfirmasi
        {/if}
      </AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
