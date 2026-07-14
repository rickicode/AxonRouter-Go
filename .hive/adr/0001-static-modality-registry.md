# ADR 0001: Static Modality Registry for the Pilot

## Status

Accepted

## Context

As part of the provider-category-mode feature, AxonRouter needs to know:

1. Which service kinds a provider type supports (e.g., `llm`, `embedding`, `image`).
2. Which model IDs are valid for each supported modality on providers that do not simply expose an OpenAI-style `/v1/models` catalog.

The first provider requiring per-modality authoring is Cloudflare Workers AI (`cf/`), which exposes separate canonical model lists for embeddings and image generation.

We evaluated two options for storing this data:

- **Database tables** managed through the admin dashboard.
- **Static JSON files** embedded in the binary, loaded once at startup.

## Decision

We will keep modality registries as static JSON files for the pilot, loaded by `internal/modalities` via `//go:embed` and `sync.Once`.

The registry is keyed by provider type ID and modality. For the Cloudflare pilot it includes the canonical embedding and image model IDs that the `/v1/embeddings` and `/v1/images/generations` handlers validate against before routing to `CloudflareExecutor` adapters.

Service-kind constants live in `internal/provider/servicekind.go` alongside `HasServiceKind` and `DefaultServiceKinds` helpers.

## Consequences

**Pros**

- No new migrations or admin UI needed while the modality taxonomy is still stabilizing.
- Lookups are in-memory and allocation-free after initial load, keeping routing latency under the 1 ms target.
- Shipping the registry with the binary guarantees that deployments cannot drift from supported model lists.

**Cons**

- Adding or removing per-modality models requires a code change and a new release.
- As the number of providers and modalities grows, maintaining JSON files by hand will not scale.

## Future Work

When the set of supported service kinds and per-modality model registries stabilizes, migrate the data to database tables surfaced through the admin provider-type UI. The in-memory loader can remain as a cache layer to preserve fast lookups.
