<script lang="ts">
  import '../app.css';
  import { page } from '$app/stores';
  import type { Snippet } from 'svelte';

  let { children }: { children: Snippet } = $props();

  // Navigation state
  let isMobileMenuOpen = $state(false);
  let isScrolled = $state(false);

  // Check scroll position for nav background
  function handleScroll() {
    isScrolled = window.scrollY > 0;
  }

  // Navigation links
  const navLinks = [
    { href: '/', label: 'Dashboard' },
    { href: '/providers', label: 'Providers' },
    { href: '/combos', label: 'Combos' },
    { href: '/logs', label: 'Logs' },
    { href: '/settings', label: 'Settings' },
  ];
</script>

<svelte:window onscroll={handleScroll} />

<div class="min-h-screen bg-canvas">
  <!-- Navigation -->
  <nav class="sticky top-0 z-50 transition-colors duration-300 {isScrolled ? 'bg-canvas border-b border-hairline' : 'bg-canvas-dark'}">
    <div class="max-w-container mx-auto px-3xl">
      <div class="flex items-center justify-between h-16">
        <!-- Logo -->
        <a href="/" class="flex items-center gap-sm">
          <span class="font-display font-medium text-display-md {isScrolled ? 'text-ink' : 'text-on-dark'}">
            AxonRouter
          </span>
          <span class="font-mono text-mono-caps-eyebrow uppercase {isScrolled ? 'text-body' : 'text-on-dark/60'}">
            GO
          </span>
        </a>

        <!-- Desktop Navigation -->
        <div class="hidden tablet:flex items-center gap-2xl">
          {#each navLinks as link}
            <a
              href={link.href}
              class="font-body text-body-md {isScrolled ? 'text-ink hover:text-primary' : 'text-on-dark hover:text-on-dark/80'} {$page.url.pathname === link.href ? 'font-medium' : ''}"
            >
              {link.label}
            </a>
          {/each}
        </div>

        <!-- CTA Buttons -->
        <div class="hidden tablet:flex items-center gap-md">
          <a href="/api/admin" class="button-outline">
            <span class="font-mono text-mono-caps-button uppercase">API</span>
          </a>
          <a href="/" class="button-primary">
            <span class="font-mono text-mono-caps-button uppercase">Dashboard</span>
          </a>
        </div>

        <!-- Mobile Menu Button -->
        <button
          class="tablet:hidden p-sm {isScrolled ? 'text-ink' : 'text-on-dark'}"
          onclick={() => isMobileMenuOpen = !isMobileMenuOpen}
          aria-label="Toggle menu"
        >
          <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            {#if isMobileMenuOpen}
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            {:else}
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
            {/if}
          </svg>
        </button>
      </div>
    </div>

    <!-- Mobile Menu -->
    {#if isMobileMenuOpen}
      <div class="tablet:hidden bg-canvas-dark border-t border-surface-dark-soft">
        <div class="px-3xl py-lg space-y-md">
          {#each navLinks as link}
            <a
              href={link.href}
              class="block font-body text-body-md text-on-dark hover:text-on-dark/80 {$page.url.pathname === link.href ? 'font-medium' : ''}"
              onclick={() => isMobileMenuOpen = false}
            >
              {link.label}
            </a>
          {/each}
          <div class="pt-md space-y-sm">
            <a href="/api/admin" class="button-outline block text-center">
              <span class="font-mono text-mono-caps-button uppercase">API</span>
            </a>
            <a href="/" class="button-primary block text-center">
              <span class="font-mono text-mono-caps-button uppercase">Dashboard</span>
            </a>
          </div>
        </div>
      </div>
    {/if}
  </nav>

  <!-- Main Content -->
  <main class="min-h-[calc(100vh-4rem)]">
    {@render children()}
  </main>

  <!-- Footer -->
  <footer class="bg-canvas border-t border-hairline">
    <div class="max-w-container mx-auto px-3xl py-section">
      <div class="grid grid-cols-1 tablet:grid-cols-4 gap-3xl">
        <!-- Brand -->
        <div class="tablet:col-span-1">
          <span class="font-display font-medium text-display-md text-ink">AxonRouter</span>
          <p class="mt-sm font-body text-body-md text-body">Universal API proxy for coding agents</p>
        </div>

        <!-- Links -->
        <div class="tablet:col-span-3 grid grid-cols-2 tablet:grid-cols-4 gap-3xl">
          <div>
            <h4 class="font-mono text-mono-caps-eyebrow uppercase text-body mb-lg">Product</h4>
            <ul class="space-y-sm">
              <li><a href="/providers" class="font-body text-body-md text-ink hover:text-primary">Providers</a></li>
              <li><a href="/combos" class="font-body text-body-md text-ink hover:text-primary">Combos</a></li>
              <li><a href="/logs" class="font-body text-body-md text-ink hover:text-primary">Logs</a></li>
            </ul>
          </div>
          <div>
            <h4 class="font-mono text-mono-caps-eyebrow uppercase text-body mb-lg">API</h4>
            <ul class="space-y-sm">
              <li><a href="/api/admin" class="font-body text-body-md text-ink hover:text-primary">Admin API</a></li>
              <li><a href="/v1/models" class="font-body text-body-md text-ink hover:text-primary">Models</a></li>
            </ul>
          </div>
          <div>
            <h4 class="font-mono text-mono-caps-eyebrow uppercase text-body mb-lg">Resources</h4>
            <ul class="space-y-sm">
              <li><a href="/docs" class="font-body text-body-md text-ink hover:text-primary">Documentation</a></li>
            </ul>
          </div>
          <div>
            <h4 class="font-mono text-mono-caps-eyebrow uppercase text-body mb-lg">Legal</h4>
            <ul class="space-y-sm">
              <li><a href="/privacy" class="font-body text-body-md text-ink hover:text-primary">Privacy</a></li>
            </ul>
          </div>
        </div>
      </div>

      <!-- Wordmark Banner -->
      <div class="mt-section pt-section border-t border-hairline">
        <div class="text-center">
          <span class="font-display font-medium text-display-xxl text-hairline select-none">axonrouter.go</span>
        </div>
      </div>
    </div>
  </footer>
</div>

<style>
  :global(.button-primary) {
    @apply bg-primary text-on-primary px-2xl py-xs rounded-sm;
    @apply font-mono text-mono-caps-button uppercase;
    @apply hover:bg-primary/90 transition-colors;
  }
  :global(.button-outline) {
    @apply bg-canvas text-ink px-2xl py-xs rounded-xs;
    @apply border border-hairline;
    @apply font-mono text-mono-caps-button uppercase;
    @apply hover:bg-hairline transition-colors;
  }
</style>
