<script lang="ts">
  import { fetchApi } from '$lib/api';
  import { setToken, authStore } from '$lib/auth';
  import { toast } from 'svelte-sonner';
  import { Input } from '$lib/components/ui/input';
  import { Button } from '$lib/components/ui/button';

  let password = $state('');
  let loading = $state(false);
  let error = $state('');

  async function submit() {
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

<div class="flex flex-1 flex-col gap-6 p-6 items-center justify-center">
  <div class="w-full max-w-sm bg-card shadow-card rounded-xl border-border border p-6 flex flex-col gap-4">
    <div class="space-y-1">
      <h1 class="text-display-md">Sign in.</h1>
      <p class="text-body-sm text-muted-foreground">Enter your admin password to access the dashboard.</p>
    </div>
    <Input
      type="password"
      placeholder="Password"
      bind:value={password}
      onkeydown={(e) => e.key === 'Enter' && submit()}
    />
    {#if error}
      <p class="text-caption text-red-500">{error}</p>
    {/if}
    <Button class="w-full" onclick={submit} disabled={loading}>
      {loading ? 'Signing in…' : 'Sign in'}
    </Button>
  </div>
</div>
