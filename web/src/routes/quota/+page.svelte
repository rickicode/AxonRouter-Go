<script lang="ts">
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Badge } from '$lib/components/ui/badge';
  import { Progress } from '$lib/components/ui/progress';
  import { Separator } from '$lib/components/ui/separator';
</script>

<svelte:head>
  <title>Quota Tracker - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-8 p-8 relative">
  <!-- Top Ambient Glows -->
  <div class="absolute top-0 left-1/4 w-[500px] h-[500px] bg-gradient-to-br from-[#007cf0]/10 via-[#7928ca]/5 to-transparent rounded-full blur-[120px] pointer-events-none"></div>

  <!-- Page header -->
  <div class="space-y-2 relative z-10">
    <div class="inline-flex items-center gap-1 bg-primary/5 text-primary border border-primary/10 rounded-full px-2.5 py-0.5 text-caption-mono font-medium">
      <span class="size-1.5 rounded-full bg-primary"></span>
      Usage Limits
    </div>
    <h1 class="text-display-lg tracking-[-0.04em]">Quota Tracker.</h1>
    <p class="text-body-sm text-muted-foreground">
      Monitor provider token allowances and monthly limits.
    </p>
  </div>

  <Card class="shadow-vercel-2 border relative z-10 bg-card/60 backdrop-blur-sm">
    <CardHeader>
      <CardTitle class="text-body-md font-semibold">Provider Quotas</CardTitle>
      <CardDescription class="text-body-sm">Aggregated limit tracking and model-specific usage rates.</CardDescription>
    </CardHeader>
    <CardContent>
      <div class="flex flex-col gap-6">
        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <span class="text-body-sm font-semibold">OpenAI</span>
              <Badge variant="secondary" class="text-caption-mono rounded-sm border bg-background/50">GPT-4</Badge>
            </div>
            <span class="text-caption-mono text-muted-foreground">75% used</span>
          </div>
          <Progress value={75} class="h-1.5" />
        </div>
        
        <Separator class="bg-border/60" />
        
        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <span class="text-body-sm font-semibold">Anthropic</span>
              <Badge variant="secondary" class="text-caption-mono rounded-sm border bg-background/50">Claude</Badge>
            </div>
            <span class="text-caption-mono text-muted-foreground">42% used</span>
          </div>
          <Progress value={42} class="h-1.5" />
        </div>
        
        <Separator class="bg-border/60" />
        
        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <span class="text-body-sm font-semibold">Google</span>
              <Badge variant="secondary" class="text-caption-mono rounded-sm border bg-background/50">Gemini</Badge>
            </div>
            <span class="text-caption-mono text-muted-foreground">18% used</span>
          </div>
          <Progress value={18} class="h-1.5" />
        </div>
      </div>
    </CardContent>
  </Card>
</div>
