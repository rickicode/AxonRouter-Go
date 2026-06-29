<script lang="ts">
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Badge } from '$lib/components/ui/badge';
  import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
</script>

<svelte:head>
  <title>Usage - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-8 p-8 relative">
  <!-- Top Ambient Glows -->
  <div class="absolute top-0 left-1/4 w-[500px] h-[500px] bg-gradient-to-br from-[#007cf0]/10 via-[#7928ca]/5 to-transparent rounded-full blur-[120px] pointer-events-none"></div>

  <!-- Page header -->
  <div class="space-y-2 relative z-10">
    <div class="inline-flex items-center gap-1 bg-primary/5 text-primary border border-primary/10 rounded-full px-2.5 py-0.5 text-caption-mono font-medium">
      <span class="size-1.5 rounded-full bg-primary"></span>
      Token Tracker
    </div>
    <h1 class="text-display-lg tracking-[-0.04em]">Usage.</h1>
    <p class="text-body-sm text-muted-foreground">
      Track API consumption, input/output tokens, and cumulative pricing profiles.
    </p>
  </div>

  <Tabs value="overview" class="w-full relative z-10">
    <TabsList class="border-b bg-transparent rounded-none h-auto p-0 gap-6 w-full justify-start">
      <TabsTrigger value="overview" class="rounded-none border-b-2 border-transparent data-[state=active]:border-foreground bg-transparent px-0 pb-3 pt-0 text-body-sm font-semibold transition-all data-[state=active]:bg-transparent data-[state=active]:shadow-none cursor-pointer">Overview</TabsTrigger>
      <TabsTrigger value="by-provider" class="rounded-none border-b-2 border-transparent data-[state=active]:border-foreground bg-transparent px-0 pb-3 pt-0 text-body-sm font-semibold transition-all data-[state=active]:bg-transparent data-[state=active]:shadow-none cursor-pointer">By Provider</TabsTrigger>
      <TabsTrigger value="by-model" class="rounded-none border-b-2 border-transparent data-[state=active]:border-foreground bg-transparent px-0 pb-3 pt-0 text-body-sm font-semibold transition-all data-[state=active]:bg-transparent data-[state=active]:shadow-none cursor-pointer">By Model</TabsTrigger>
    </TabsList>
    
    <TabsContent value="overview" class="mt-6 space-y-6">
      <div class="grid gap-6 md:grid-cols-3">
        <Card class="shadow-vercel-2 border bg-card/60 backdrop-blur-sm transition-all hover:shadow-vercel-3">
          <CardHeader class="pb-2">
            <CardDescription class="text-caption-mono uppercase font-semibold">Total Requests</CardDescription>
          </CardHeader>
          <CardContent>
            <div class="text-display-sm font-semibold">12,847</div>
            <p class="text-xs text-emerald-500 font-mono mt-1">+20.1% from last month</p>
          </CardContent>
        </Card>
        
        <Card class="shadow-vercel-2 border bg-card/60 backdrop-blur-sm transition-all hover:shadow-vercel-3">
          <CardHeader class="pb-2">
            <CardDescription class="text-caption-mono uppercase font-semibold">Total Tokens</CardDescription>
          </CardHeader>
          <CardContent>
            <div class="text-display-sm font-semibold">4.2M</div>
            <p class="text-xs text-emerald-500 font-mono mt-1">+12.5% from last month</p>
          </CardContent>
        </Card>
        
        <Card class="shadow-vercel-2 border bg-card/60 backdrop-blur-sm transition-all hover:shadow-vercel-3">
          <CardHeader class="pb-2">
            <CardDescription class="text-caption-mono uppercase font-semibold">Total Cost</CardDescription>
          </CardHeader>
          <CardContent>
            <div class="text-display-sm font-semibold">$284.50</div>
            <p class="text-xs text-[#ff0080] font-mono mt-1">+8.3% from last month</p>
          </CardContent>
        </Card>
      </div>
    </TabsContent>
    
    <TabsContent value="by-provider" class="mt-6">
      <Card class="shadow-vercel-2 border bg-card/40">
        <CardContent class="py-12 text-center text-muted-foreground text-body-sm">
          Provider usage breakdown will be displayed here.
        </CardContent>
      </Card>
    </TabsContent>
    
    <TabsContent value="by-model" class="mt-6">
      <Card class="shadow-vercel-2 border bg-card/40">
        <CardContent class="py-12 text-center text-muted-foreground text-body-sm">
          Model usage breakdown will be displayed here.
        </CardContent>
      </Card>
    </TabsContent>
  </Tabs>
</div>
