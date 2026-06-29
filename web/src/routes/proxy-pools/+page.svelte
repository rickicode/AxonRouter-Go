<script lang="ts">
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button';
</script>

<svelte:head>
  <title>Proxy Pools - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-8 p-8 relative">
  <!-- Top Ambient Glows -->
  <div class="absolute top-0 right-1/4 w-[400px] h-[400px] bg-gradient-to-br from-[#007cf0]/10 via-[#00dfd8]/5 to-transparent rounded-full blur-[100px] pointer-events-none"></div>

  <!-- Page header -->
  <div class="space-y-2 relative z-10">
    <div class="inline-flex items-center gap-1 bg-primary/5 text-primary border border-primary/10 rounded-full px-2.5 py-0.5 text-caption-mono font-medium">
      <span class="size-1.5 rounded-full bg-primary"></span>
      Failover Pools
    </div>
    <h1 class="text-display-lg tracking-[-0.04em]">Proxy Pools.</h1>
    <p class="text-body-sm text-muted-foreground">
      Group multiple connection keys for load balancing, failure rotation, and concurrency fallback.
    </p>
  </div>

  <div class="space-y-6 relative z-10">
    <div class="flex items-center justify-between">
      <h2 class="text-display-sm tracking-tight">Active Pools</h2>
      <Button variant="default" size="sm" class="h-8 text-xs cursor-pointer">Create Pool</Button>
    </div>

    <div class="grid gap-6 md:grid-cols-2">
      <!-- Pool 1 -->
      <Card class="shadow-vercel-2 border bg-card/60 backdrop-blur-sm transition-all hover:shadow-vercel-3">
        <CardHeader class="pb-3">
          <div class="flex items-center justify-between">
            <CardTitle class="text-body-md font-semibold">openai-high-tpm</CardTitle>
            <Badge variant="default" class="text-xs rounded-sm">Active</Badge>
          </div>
          <CardDescription class="text-body-sm">Combines 3 OpenAI keys with high token-per-minute limits.</CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-2 text-xs text-muted-foreground font-mono">
            <div class="flex justify-between">
              <span>Strategy:</span>
              <span class="text-foreground">Round-Robin</span>
            </div>
            <div class="flex justify-between">
              <span>Keys Count:</span>
              <span class="text-foreground">3 Keys</span>
            </div>
          </div>
          <Button variant="outline" size="sm" class="h-8 text-xs w-full cursor-pointer font-medium border-border/80">Manage Pool</Button>
        </CardContent>
      </Card>

      <!-- Pool 2 -->
      <Card class="shadow-vercel-2 border bg-card/60 backdrop-blur-sm transition-all hover:shadow-vercel-3">
        <CardHeader class="pb-3">
          <div class="flex items-center justify-between">
            <CardTitle class="text-body-md font-semibold">claude-failover-pool</CardTitle>
            <Badge variant="default" class="text-xs rounded-sm">Active</Badge>
          </div>
          <CardDescription class="text-body-sm">Balances traffic between primary Claude 3.5 keys and API proxies.</CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-2 text-xs text-muted-foreground font-mono">
            <div class="flex justify-between">
              <span>Strategy:</span>
              <span class="text-foreground">Fallback Failover</span>
            </div>
            <div class="flex justify-between">
              <span>Keys Count:</span>
              <span class="text-foreground">2 Keys</span>
            </div>
          </div>
          <Button variant="outline" size="sm" class="h-8 text-xs w-full cursor-pointer font-medium border-border/80">Manage Pool</Button>
        </CardContent>
      </Card>
    </div>
  </div>
</div>
