<script lang="ts">
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button';
  import { Switch } from '$lib/components/ui/switch';
  import { Label } from '$lib/components/ui/label';
</script>

<svelte:head>
  <title>Endpoints - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-8 p-8 relative">
  <!-- Top Ambient Glows -->
  <div class="absolute top-0 right-1/4 w-[400px] h-[400px] bg-gradient-to-br from-[#7928ca]/10 via-[#ff0080]/5 to-transparent rounded-full blur-[100px] pointer-events-none"></div>

  <!-- Page header -->
  <div class="space-y-2 relative z-10">
    <div class="inline-flex items-center gap-1 bg-primary/5 text-primary border border-primary/10 rounded-full px-2.5 py-0.5 text-caption-mono font-medium">
      <span class="size-1.5 rounded-full bg-primary"></span>
      Gateway Endpoints
    </div>
    <h1 class="text-display-lg tracking-[-0.04em]">Endpoints.</h1>
    <p class="text-body-sm text-muted-foreground">
      Configure downstream translation targets and active path mappings.
    </p>
  </div>

  <div class="grid gap-6 md:grid-cols-2 relative z-10">
    <!-- OpenAI Card -->
    <Card class="shadow-vercel-2 border bg-card/60 backdrop-blur-sm transition-all hover:shadow-vercel-3">
      <CardHeader>
        <CardTitle class="text-body-md font-semibold">OpenAI Compatible</CardTitle>
        <CardDescription class="text-body-sm">Expose standard OpenAI SDK compatibility layers.</CardDescription>
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        <div class="flex items-center justify-between">
          <div class="space-y-0.5">
            <Label class="text-body-sm font-medium">Chat Completions</Label>
            <p class="text-xs text-muted-foreground">Enable `/v1/chat/completions` pathway.</p>
          </div>
          <Switch checked />
        </div>
        <div class="rounded-md bg-muted p-3 font-mono text-xs border">
          /v1/chat/completions
        </div>
      </CardContent>
    </Card>

    <!-- Anthropic Card -->
    <Card class="shadow-vercel-2 border bg-card/60 backdrop-blur-sm transition-all hover:shadow-vercel-3">
      <CardHeader>
        <CardTitle class="text-body-md font-semibold">Anthropic Compatible</CardTitle>
        <CardDescription class="text-body-sm">Expose standard Anthropic SDK messages pathway.</CardDescription>
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        <div class="flex items-center justify-between">
          <div class="space-y-0.5">
            <Label class="text-body-sm font-medium">Messages Routing</Label>
            <p class="text-xs text-muted-foreground">Enable `/v1/messages` pathway.</p>
          </div>
          <Switch checked />
        </div>
        <div class="rounded-md bg-muted p-3 font-mono text-xs border">
          /v1/messages
        </div>
      </CardContent>
    </Card>
  </div>

  <!-- Active list -->
  <Card class="shadow-vercel-2 border relative z-10 bg-card/40">
    <CardHeader>
      <CardTitle class="text-body-md font-semibold">Gateway Routing Registry</CardTitle>
      <CardDescription class="text-body-sm">Currently active and accessible request pathways.</CardDescription>
    </CardHeader>
    <CardContent>
      <div class="divide-y divide-border border rounded-md overflow-hidden bg-card/80">
        <div class="flex items-center justify-between p-4 hover:bg-accent/10 transition-colors">
          <div class="flex items-center gap-3">
            <Badge variant="default" class="text-xs rounded-sm">Active</Badge>
            <span class="font-mono text-body-sm text-foreground">/v1/chat/completions</span>
          </div>
          <span class="text-caption-mono text-muted-foreground uppercase">OpenAI</span>
        </div>
        <div class="flex items-center justify-between p-4 hover:bg-accent/10 transition-colors">
          <div class="flex items-center gap-3">
            <Badge variant="default" class="text-xs rounded-sm">Active</Badge>
            <span class="font-mono text-body-sm text-foreground">/v1/messages</span>
          </div>
          <span class="text-caption-mono text-muted-foreground uppercase">Anthropic</span>
        </div>
      </div>
    </CardContent>
  </Card>
</div>
