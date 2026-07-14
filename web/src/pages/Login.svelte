<script lang="ts">
import { fetchApi } from '$lib/api';
import { setToken, authStore } from '$lib/auth';
  import { toast } from 'svelte-sonner';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Button } from '$lib/components/ui/button';
  import LockIcon from '@lucide/svelte/icons/lock';
  import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
  import EyeIcon from '@lucide/svelte/icons/eye';
  import EyeOffIcon from '@lucide/svelte/icons/eye-off';
  import Loader2Icon from '@lucide/svelte/icons/loader-2';

  let password = $state('');
  let show = $state(false);
  let loading = $state(false);
  let error = $state('');

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    loading = true;
    error = '';
    try {
	const res = await fetchApi<{ token: string }>('/login', {
		method: 'POST',
		body: JSON.stringify({ password }),
	});
	if (res.token) {
		setToken(res.token);
		authStore.set(true);
	}

    } catch (e) {
      error = e instanceof Error ? e.message : 'Login failed';
      toast.error(error);
    } finally {
      loading = false;
    }
  }
</script>

<div class="relative flex min-h-screen flex-col items-center justify-center overflow-hidden bg-background p-6">
  <!-- ambient gradient glow blobs -->
  <div class="pointer-events-none absolute -top-32 -left-24 h-96 w-96 rounded-full bg-primary/10 blur-3xl"></div>
  <div class="pointer-events-none absolute -bottom-32 -right-24 h-96 w-96 rounded-full bg-primary/10 blur-3xl"></div>

  <div class="relative w-full max-w-sm">
    <div class="flex flex-col gap-6 rounded-2xl border border-border bg-card/80 p-8 shadow-card backdrop-blur-xl">
      <!-- brand mark + heading -->
      <div class="flex flex-col items-center gap-3 text-center">
        <div class="flex size-12 items-center justify-center rounded-xl border border-border bg-background/50">
          <img src="/logo.svg" alt="AxonRouter" class="size-7" />
        </div>
        <div class="space-y-1">
          <h1 class="text-display-md">Sign in.</h1>
          <p class="text-body-sm text-muted-foreground">
            Enter your admin password to access the dashboard.
          </p>
        </div>
      </div>

      <form class="flex flex-col gap-4" onsubmit={submit}>
        <div class="flex flex-col gap-2">
          <Label for="password" class="text-body-sm-strong">Password</Label>
          <div class="relative">
            <Input
              id="password"
              type={show ? 'text' : 'password'}
              placeholder="Enter your password"
              autocomplete="current-password"
              class="h-11 pr-10"
              bind:value={password}
            />
            <button
              type="button"
              class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground transition-colors hover:text-foreground"
              onclick={() => (show = !show)}
              aria-label={show ? 'Hide password' : 'Show password'}
            >
              {#if show}
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

        <Button type="submit" class="h-11 w-full gap-2" disabled={loading}>
          {#if loading}
            <Loader2Icon class="size-4 animate-spin" />
            <span>Signing in…</span>
          {:else}
            <LockIcon class="size-4" />
            <span>Sign in</span>
          {/if}
        </Button>
      </form>

      <!-- footer hint -->
      <div class="flex items-start gap-2 rounded-lg border border-border bg-background/40 px-3 py-2 text-caption text-muted-foreground">
        <ShieldCheckIcon class="mt-0.5 size-4 shrink-0 text-primary" />
    <span>
      The initial admin password is 12345677. Change it from Settings or via the CLI (axonrouter --setpass &lt;password&gt;).
    </span>
      </div>
    </div>
  </div>
</div>
