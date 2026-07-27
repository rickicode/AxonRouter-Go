<script lang="ts">
import { Label } from '$lib/components/ui/label';
import { Button } from '$lib/components/ui/button';
import { Textarea } from '$lib/components/ui/textarea';
import type { CLIToolConfig } from '$lib/api';
import Copy from '@lucide/svelte/icons/copy';
import Check from '@lucide/svelte/icons/check';

interface Props {
	config: CLIToolConfig | null;
	copiedField: string | null;
	onCopy: (text: string, field: string) => void;
}

let { config, copiedField, onCopy }: Props = $props();
</script>

{#if config}
	<div id="generated-section" class="space-y-4 rounded-lg border border-border bg-background/50 p-4">
		<h3 class="text-body-md-strong">Generated config</h3>
		{#if config.envBlock}
			<div class="space-y-2">
				<div class="flex items-center justify-between">
					<Label class="text-caption-mono text-muted-foreground">Environment variables</Label>
					<Button
						variant="ghost"
						size="sm"
						class="h-7 gap-1.5 text-caption"
						onclick={() => onCopy(config.envBlock, 'env')}
					>
						{#if copiedField === 'env'}
							<Check class="size-3.5" /> Copied
						{:else}
							<Copy class="size-3.5" /> Copy
						{/if}
					</Button>
				</div>
				<Textarea
					readonly
					value={config.envBlock}
					rows={Math.min(10, config.envBlock.split('\n').length)}
					class="font-mono text-body-sm bg-background"
				/>
			</div>
		{/if}
		{#if config.configContent}
			<div class="space-y-2">
				<div class="flex items-center justify-between">
					<div class="flex flex-col gap-1">
						<Label class="text-caption-mono text-muted-foreground">Config file path</Label>
						{#if config.configPath}
							<div class="flex items-center gap-2">
								<code class="text-body-sm font-mono text-muted-foreground">{config.configPath}</code>
								<Button
									variant="ghost"
									size="sm"
									class="h-7 gap-1.5 text-caption"
									onclick={() => onCopy(config.configPath, 'path')}
								>
									{#if copiedField === 'path'}
										<Check class="size-3.5" /> Copied
									{:else}
										<Copy class="size-3.5" /> Copy path
									{/if}
								</Button>
							</div>
						{/if}
					</div>
					<div class="flex items-center gap-1">
						{#if config.configPath}
							<Button
								variant="ghost"
								size="sm"
								class="h-7 gap-1.5 text-caption"
								onclick={() => config.configPath && onCopy(config.configPath, 'path')}
							>
								{#if copiedField === 'path'}
									<Check class="size-3.5" /> Copied path
								{:else}
									<Copy class="size-3.5" /> Copy path
								{/if}
							</Button>
						{/if}
						<Button
							variant="ghost"
							size="sm"
							class="h-7 gap-1.5 text-caption"
							onclick={() => onCopy(config.configContent, 'config')}
						>
							{#if copiedField === 'config'}
								<Check class="size-3.5" /> Copied
							{:else}
								<Copy class="size-3.5" /> Copy
							{/if}
						</Button>
					</div>
				</div>
				<Textarea
					readonly
					value={config.configContent}
					rows={Math.min(14, config.configContent.split('\n').length)}
					class="font-mono text-body-sm bg-background"
				/>
				{#if config.backupPath}
					<p class="mt-1 text-caption text-muted-foreground">
						Backup tersimpan di: <code class="font-mono">{config.backupPath}</code>
					</p>
				{/if}
			</div>
		{/if}
		{#if config.runCommand}
			<div class="space-y-2">
				<Label class="text-caption-mono text-muted-foreground">Example command</Label>
				<div class="rounded-md border border-border bg-background px-3 py-2 font-mono text-body-sm">
					{config.runCommand}
				</div>
			</div>
		{/if}
		{#if config.extraFiles && config.extraFiles.length > 0}
			<div class="space-y-3">
				<h4 class="text-body-sm-strong">Additional files</h4>
				{#each config.extraFiles as file}
					<div class="space-y-2">
						<div class="flex items-center justify-between">
							<Label class="text-caption-mono text-muted-foreground">
								{#if file.path}
									<span class="text-muted-foreground/70">{file.path}</span>
								{:else}
									Extra file
								{/if}
							</Label>
							<Button
								variant="ghost"
								size="sm"
								class="h-7 gap-1.5 text-caption"
								onclick={() => onCopy(file.content, `extra-${file.path}`)}
							>
								{#if copiedField === `extra-${file.path}`}
									<Check class="size-3.5" /> Copied
								{:else}
									<Copy class="size-3.5" /> Copy
								{/if}
							</Button>
						</div>
						<Textarea
							readonly
							value={file.content}
							rows={Math.min(10, file.content.split('\n').length)}
							class="font-mono text-body-sm bg-background"
						/>
					</div>
				{/each}
			</div>
		{/if}
	</div>
{/if}
