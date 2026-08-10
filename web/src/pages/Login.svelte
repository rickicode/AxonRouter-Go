<script lang="ts">
  import { fetchApi } from '$lib/api';
  import { setToken, setRememberMe, authStore, setMustChangePassword } from '$lib/auth';
  import { toast } from 'svelte-sonner';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Button } from '$lib/components/ui/button';
  import LockIcon from '@lucide/svelte/icons/lock';
  import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
  import EyeIcon from '@lucide/svelte/icons/eye';
  import EyeOffIcon from '@lucide/svelte/icons/eye-off';
  import Loader2Icon from '@lucide/svelte/icons/loader-2';
  import AxonIcon from '@lucide/svelte/icons/cpu';

  let password = $state('');
  let show = $state(false);
  let loading = $state(false);
  let error = $state('');

  interface LoginResponse {
    token: string;
    mustChangePassword: boolean;
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    loading = true;
    error = '';
    try {
      const res = await fetchApi<LoginResponse>('/login', {
        method: 'POST',
        body: JSON.stringify({ password, remember_me: true }),
      });
      if (res.token) {
        setRememberMe(true);
        setToken(res.token, true);
        setMustChangePassword(res.mustChangePassword, true);
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

<svelte:head>
  <title>Sign in · AxonRouter</title>
</svelte:head>

<div
  class="relative flex min-h-screen flex-col items-center justify-center overflow-hidden bg-background p-6"
>
  <!-- Brand mesh gradient matching dashboard vibe -->
  <div
    class="pointer-events-none absolute inset-0 gradient-mesh opacity-60"
    aria-hidden="true"
  ></div>
  <div
    class="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_50%_120%,rgba(236,72,153,0.16),transparent_55%)]"
    aria-hidden="true"
  ></div>

  <div class="relative z-10 w-full max-w-[22rem]">
    <!-- Card -->
    <div
      class="flex flex-col gap-6 rounded-2xl border border-border bg-card/80 p-8 shadow-elevated backdrop-blur-xl"
    >
      <!-- Brand mark + heading -->
      <div class="flex flex-col items-center gap-4 text-center">
        <div
          class="flex size-14 items-center justify-center rounded-xl bg-gradient-to-br from-primary to-pink-600 shadow-lg shadow-primary/25"
        >
          <AxonIcon class="size-7 text-white" />
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

        <Button type="submit" size="lg" class="h-11 w-full gap-2" disabled={loading}>
          {#if loading}
            <Loader2Icon class="size-4 animate-spin" />
            <span>Signing in…</span>
          {:else}
            <LockIcon class="size-4" />
            <span>Sign in</span>
          {/if}
        </Button>
      </form>

      <!-- Footer hint -->
      <div
        class="flex items-start gap-2.5 rounded-lg border border-border bg-background/40 px-3 py-2.5 text-caption text-muted-foreground"
      >
        <ShieldCheckIcon class="mt-0.5 size-4 shrink-0 text-primary" />
        <span>
          The initial admin password is <span class="font-mono text-foreground">12345677</span>. Change it from Settings or via the CLI.
        </span>
      </div>
    </div>

    <!-- Subtle footer -->
    <p class="mt-5 text-center text-caption text-muted-foreground/60">
      AxonRouter Dashboard · v0.3.10
    </p>
  </div>
</div>
