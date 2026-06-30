---
version: alpha
name: AxonRouter Dark
website: "https://vercel.com"
description: A dark-only design system inspired by Vercel's design language — deep zinc-black surfaces (#0f0f12), pink primary accent (#ec4899) for all interactive elements, and a multi-color mesh gradient (cyan / blue / magenta / amber) as the sole decorative system. Hardcoded dark theme with no light mode toggle.

seo:
  title: "AxonRouter Dark Design System — Pink #ec4899, Dark Zinc Surfaces, Geist Typography"
  metaDescription: "Dark-only design system for AxonRouter. Pink primary #ec4899, zinc-dark surfaces #0f0f12/#18181b, Geist + Geist Mono, mesh gradient, 40+ components."
  highlights:
    - "Dark-only — #0f0f12 page background, #18181b card surfaces, no light mode toggle"
    - "Pink primary — #ec4899 carries every CTA, active state, and interactive accent on dark surfaces"
    - "One mesh gradient — cyan #50e3c2, blue #007cf0, pink #ff0080, amber #f9cb28 fused, used at hero scale"
    - "Geist display caps at weight 600 — aggressive -2.4px tracking carries voice instead of heavier weights"
    - "White inset hairline rings — subtle border glow on dark cards instead of dark-on-light borders"
  tags:
    - "Web Infrastructure & Hosting"
    - "Developer Tools & IDEs"
  lastUpdated: "2026-05-12"
  author:
    name: "Dov Azencot"
    url: "https://x.com/dovazencot"
  opening: |
    Vercel's design language is the dashboard marketing surface for a developer platform, written for engineers who already know the syntax. The page operates with one of the strictest stark systems on the web: a near-white #fafafa body, ink-near-black #171717 for type, and a 200-step gray scale where every divider, border, and disabled state lives on its own deliberate step. There is no brand-blue or marketing accent — the ink IS the brand. Conversion targets, dark bands, code mockups, and primary CTAs all share the same #171717 tone, polarity-flipped onto white when a section needs depth.

    This DESIGN.md packages the system into a single machine-readable file. Inside: 40 color tokens (including the three-pair Develop / Preview / Ship gradient stack), 15 typography styles across Geist and Geist Mono, 9 corner radii (from 0px to 9999px, with the 100px pill as the marketing-CTA signature), 12 spacing values stepping from 4px to a 192px section gap, and 40+ components from `nav-bar` to `pricing-card-featured` to the polarity-flipped `showcase-band-dark`. The format follows the Google Labs DESIGN.md spec — colors, typography, rounded, spacing, components, all token-referenced.

    Feed the file to Claude, Cursor, or Copilot when you need a React component that reads as Vercel rather than as a generic shadcn theme. The agent picks up the discipline — 100px pill CTAs, sentence-case headlines with -2.4px tracking, mono eyebrows above geometric-sans body, stacked shadows instead of heavy drops. Reference the tokens directly in Tailwind config, or use the spec as an audit checklist. The system is worth studying because of what it refuses: no second accent color, no display weight above 600, no gradient miniaturization. Restraint is the product.
  related:
    - href: "https://vercel.com/design"
      title: "Vercel's design site"
      description: "The Geist design system and brand resources direct from Vercel."
    - href: "/design"
      title: "Browse all design systems"
      description: "The full directory of DESIGN.md files on shadcn.io, with live mockups for each."
    - href: "/blocks"
      title: "React blocks for shadcn/ui"
      description: "Production-ready hero, pricing, CTA, and dashboard sections built with the same Tailwind + shadcn primitives."
  questions:
    - id: "primary-color"
      title: "What is AxonRouter's primary brand color?"
      answer: "AxonRouter's primary is #ec4899 — a vibrant pink (Tailwind pink-500) that carries every CTA, active sidebar indicator, focus ring, and interactive accent on dark surfaces (#0f0f12 background). It achieves WCAG AA contrast on dark backgrounds. Links use #f472b6 (pink-400), one step lighter for inline readability."
    - id: "gradient"
      title: "What is the brand mesh gradient and where should I use it?"
      answer: "The signature decoration is a three-pair gradient stack: Develop (#007cf0 to #00dfd8), Preview (#7928ca to #ff0080), and Ship (#ff4d4d to #f9cb28). The three pairs fuse into a single multi-color mesh that floats behind the hero band and inside feature-band atmospheric backdrops. Treat it as one unified object — never crop to a single hue, never miniaturize to an icon, never reorder the stops. It lives at hero scale only."
    - id: "typography"
      title: "What typography does Vercel use, and what should I use if Geist isn't available?"
      answer: "The system runs Geist for display, body, button, and label, and Geist Mono for terminal mockups, code blocks, and technical eyebrows. Geist sits at weights 400 / 500 / 600 only — the display ceiling is 600, never 700+. Display sizes track aggressively negative (-2.4px at 48px hero, -1.28px at 32px section). If Geist is unavailable, Inter with `font-feature-settings: 'ss01', 'ss02'` is the closest geometric-sans substitute; JetBrains Mono at 12–13px stands in for Geist Mono."
    - id: "pill-scales"
      title: "Why does the system use two different button radii?"
      answer: "Vercel deliberately runs two pill scales side by side. Marketing CTAs (`button-primary`, `button-secondary`) use a 100px radius — fully pilled, ~48px tall. In-app nav buttons (`nav-cta-signup`, `nav-cta-login`, `nav-cta-ask-ai`) use a 6px square-ish radius — the brand's `--geist-radius`. The two scales never mix on the same screen; marketing surfaces stay pilled, app surfaces stay squared. Picking one and sticking with it is part of the voice."
    - id: "elevation"
      title: "How does AxonRouter handle elevation and shadows on dark backgrounds?"
      answer: "The system uses white inset hairline rings (rgba(255,255,255,0.06)) instead of dark borders on dark backgrounds. Every elevated card combines this inset ring with 2–3 stacked dark offsets (rgba(0,0,0,0.15-0.2)). The result is a card that glows faintly at its edges against the dark page. Five levels are documented — flat, inset hairline, subtle drop, soft stack, float stack, modal."
    - id: "use-in-project"
      title: "Can I use this DESIGN.md to build my own project?"
      answer: "Yes — the file is structured for direct ingestion by Claude, Cursor, or any AI tool that reads token-based design specs. The agent will reproduce AxonRouter's dark-only system (dark zinc surfaces, pink primary accent, mesh gradient at hero scale only, Geist at weight 600 max). Every color hex, typography size, radius, and spacing value is a quoted token you can paste straight into Tailwind config, CSS variables, or a custom component library."

colors:
  primary: "#ec4899"
  on-primary: "#ffffff"
  ink: "#e4e4e7"
  body: "#a1a1aa"
  mute: "#71717a"
  hairline: "#27272a"
  hairline-strong: "#3f3f46"
  canvas: "#18181b"
  canvas-soft: "#0f0f12"
  canvas-soft-2: "#1e1e22"
  link: "#f472b6"
  link-deep: "#ec4899"
  link-bg-soft: "#ec489920"
  success: "#4ade80"
  error: "#f87171"
  error-soft: "#f8717120"
  error-deep: "#ef4444"
  warning: "#fbbf24"
  warning-soft: "#fbbf2420"
  warning-deep: "#f59e0b"
  violet: "#a78bfa"
  violet-soft: "#a78bfa20"
  violet-deep: "#7c3aed"
  cyan: "#50e3c2"
  cyan-soft: "#50e3c220"
  cyan-deep: "#29bc9b"
  highlight-pink: "#ff0080"
  highlight-magenta: "#eb367f"
  gradient-develop-start: "#007cf0"
  gradient-develop-end: "#00dfd8"
  gradient-preview-start: "#7928ca"
  gradient-preview-end: "#ff0080"
  gradient-ship-start: "#ff4d4d"
  gradient-ship-end: "#f9cb28"
  selection-bg: "#ec4899"
  selection-fg: "#ffffff"

typography:
  display-xl:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 48px
    fontWeight: 600
    lineHeight: 48px
    letterSpacing: -2.4px
  display-lg:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 32px
    fontWeight: 600
    lineHeight: 40px
    letterSpacing: -1.28px
  display-md:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 24px
    fontWeight: 600
    lineHeight: 32px
    letterSpacing: -0.96px
  display-sm:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 20px
    fontWeight: 600
    lineHeight: 28px
    letterSpacing: -0.6px
  body-lg:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 18px
    fontWeight: 400
    lineHeight: 28px
    letterSpacing: 0px
  body-md:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 16px
    fontWeight: 400
    lineHeight: 24px
  body-md-strong:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 16px
    fontWeight: 500
    lineHeight: 24px
  body-sm:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 14px
    fontWeight: 400
    lineHeight: 20px
    letterSpacing: -0.28px
  body-sm-strong:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 14px
    fontWeight: 500
    lineHeight: 20px
    letterSpacing: -0.28px
  caption:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 12px
    fontWeight: 400
    lineHeight: 16px
  caption-mono:
    fontFamily: Geist Mono, ui-monospace, SFMono-Regular, Menlo, Monaco, monospace
    fontSize: 12px
    fontWeight: 400
    lineHeight: 16px
  code:
    fontFamily: Geist Mono, ui-monospace, SFMono-Regular, Menlo, Monaco, monospace
    fontSize: 13px
    fontWeight: 400
    lineHeight: 20px
  button-md:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 14px
    fontWeight: 500
    lineHeight: 20px
  button-lg:
    fontFamily: Geist, Inter, system-ui, -apple-system, sans-serif
    fontSize: 16px
    fontWeight: 500
    lineHeight: 24px

rounded:
  none: 0px
  xs: 4px
  sm: 6px
  md: 8px
  lg: 12px
  xl: 16px
  pill-sm: 64px
  pill: 100px
  full: 9999px

spacing:
  xxs: 4px
  xs: 8px
  sm: 12px
  md: 16px
  lg: 24px
  xl: 32px
  2xl: 40px
  3xl: 48px
  4xl: 64px
  5xl: 96px
  6xl: 128px
  section: 192px

components:
  nav-bar:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body-sm}"
    height: 64px
    padding: "{spacing.sm} {spacing.lg}"
  nav-link:
    textColor: "{colors.body}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.full}"
    padding: "{spacing.xs} {spacing.sm}"
  nav-cta-signup:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.body-sm-strong}"
    rounded: "{rounded.sm}"
    padding: "0px {spacing.xs}"
    height: 28px
  nav-cta-login:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body-sm-strong}"
    rounded: "{rounded.sm}"
    padding: "0px {spacing.xs}"
    height: 28px
  nav-cta-ask-ai:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.body-sm-strong}"
    rounded: "{rounded.sm}"
    padding: "0px {spacing.xs}"
    height: 28px
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.button-lg}"
    rounded: "{rounded.pill}"
    padding: "0px {spacing.sm}"
  button-secondary:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.button-lg}"
    rounded: "{rounded.pill}"
    padding: "0px {spacing.sm}"
  button-primary-sm:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.button-md}"
    rounded: "{rounded.pill}"
    padding: "0px {spacing.xs}"
  button-secondary-sm:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.button-md}"
    rounded: "{rounded.pill}"
    padding: "0px {spacing.xs}"
  tab-ghost:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.pill-sm}"
    padding: "0px {spacing.md}"
  icon-button-circular:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    rounded: "{rounded.full}"
  card-marketing:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    padding: "{spacing.lg}"
  card-marketing-large:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body-md}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xl}"
  card-soft:
    backgroundColor: "{colors.canvas-soft}"
    textColor: "{colors.ink}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    padding: "{spacing.lg}"
  template-card:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    padding: "{spacing.md}"
  code-editor-mockup:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.code}"
    rounded: "{rounded.md}"
    padding: "{spacing.lg}"
  form-input:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.sm}"
    padding: "0px {spacing.sm}"
    height: 40px
  form-input-sm:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.sm}"
    padding: "0px {spacing.sm}"
    height: 32px
  form-input-lg:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.body-md}"
    rounded: "{rounded.sm}"
    padding: "0px {spacing.sm}"
    height: 48px
  badge-secondary:
    backgroundColor: "{colors.canvas-soft}"
    textColor: "{colors.body}"
    typography: "{typography.caption}"
    rounded: "{rounded.full}"
    padding: "0px {spacing.xs}"
  pricing-card:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body-md}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xl}"
  pricing-card-featured:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.body-md}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xl}"
  logo-strip:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.body}"
    typography: "{typography.body-sm}"
    padding: "{spacing.lg} {spacing.xl}"
  hero-band:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.display-xl}"
    padding: "{spacing.4xl} {spacing.lg}"
  feature-mesh-band:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.display-lg}"
    padding: "{spacing.5xl} {spacing.lg}"
  showcase-band-light:
    backgroundColor: "{colors.canvas-soft}"
    textColor: "{colors.ink}"
    typography: "{typography.display-lg}"
    padding: "{spacing.5xl} {spacing.lg}"
  showcase-band-dark:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.display-lg}"
    padding: "{spacing.5xl} {spacing.lg}"
  footer:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.body}"
    typography: "{typography.body-sm}"
    padding: "{spacing.4xl} {spacing.lg}"
  link-inline:
    textColor: "{colors.link}"
    typography: "{typography.body-md}"
  banner-marketing:
    backgroundColor: "{colors.canvas-soft}"
    textColor: "{colors.body}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.full}"
    padding: "{spacing.xs} {spacing.sm}"

  # ─── Examples (illustrative) — auto-derived; resolve any TO_FILL markers below ───
  ex-pricing-tier:
    description: "Default tier card. Mirrors pricing-card chrome on canvas-soft surface with a hairline border."
    backgroundColor: "{colors.canvas-soft}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xl}"
  ex-pricing-tier-featured:
    description: "Featured tier — polarity-flipped to ink primary with white text and white CTA."
    backgroundColor: "{colors.ink}"
    textColor: "{colors.on-primary}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xl}"
  ex-product-selector:
    description: "What's Included summary card — repurposed for the brand's GPU / inference / Pro feature tiers."
    backgroundColor: "{colors.canvas-soft}"
    rounded: "{rounded.md}"
    padding: "{spacing.lg}"
  ex-cart-drawer:
    description: "Subscription summary — line items per add-on (NOT a literal e-commerce cart)."
    backgroundColor: "{colors.canvas}"
    rounded: "{rounded.md}"
    padding: "{spacing.lg}"
    item-divider: "{colors.hairline}"
  ex-app-shell-row:
    description: "Sidebar nav row. Active state uses brand primary as a left-edge indicator bar."
    backgroundColor: "{colors.canvas}"
    activeIndicator: "{colors.primary}"
    rounded: "{rounded.sm}"
    padding: "{spacing.xs} {spacing.sm}"
  ex-data-table-cell:
    description: "Mirrors the brand's table chrome. Header uses caption-mono uppercase mono; body uses body-sm."
    headerBackground: "{colors.canvas-soft}"
    headerTypography: "{typography.caption-mono}"
    bodyTypography: "{typography.body-sm}"
    cellPadding: "{spacing.xs} {spacing.sm}"
    rowBorder: "{colors.hairline}"
  ex-auth-form-card:
    description: "Sign-in / sign-up card. Mirrors card-marketing-large chrome with form-input primitives inside."
    backgroundColor: "{colors.canvas-soft}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xl}"
  ex-modal-card:
    description: "Modal dialog surface — same chrome as card-marketing-large with Level 5 modal shadow."
    backgroundColor: "{colors.canvas}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xl}"
  ex-empty-state-card:
    description: "Empty-state illustration frame. Generous padding on canvas-soft."
    backgroundColor: "{colors.canvas-soft}"
    rounded: "{rounded.lg}"
    padding: "{spacing.3xl}"
    captionTypography: "{typography.body-md}"
  ex-toast:
    description: "Toast notification surface — flat-cornered card-marketing chrome with Level 4 shadow."
    backgroundColor: "{colors.canvas}"
    rounded: "{rounded.md}"
    padding: "{spacing.sm} {spacing.md}"
    typography: "{typography.body-sm}"

---

## Overview

AxonRouter uses a dark-only design system — deep zinc-black surfaces with pink as the primary interactive accent. The system is hardcoded to dark; there is no light mode toggle. The page sits on `{colors.canvas-soft}` (`#0f0f12`) near-black background, with `{colors.canvas}` (`#18181b`) card surfaces one step lighter, and `{colors.canvas-soft-2}` (`#1e1e22`) for inset regions. Text uses a zinc-gray hierarchy: `{colors.ink}` (`#e4e4e7`) for headings, `{colors.body}` (`#a1a1aa`) for secondary text, `{colors.mute}` (`#71717a`) for the lowest-priority labels. Every divider and border lives on `{colors.hairline}` (`#27272a`), a subtle dark line that reads as a hairline without competing with content.

The primary accent is `{colors.primary}` (`#ec4899`, pink-500) — a vibrant pink that carries every CTA, active sidebar indicator, focus ring, and interactive highlight. On dark surfaces it achieves WCAG AA contrast and reads as the brand's signature color. Links use `{colors.link}` (`#f472b6`, pink-400), one step lighter for inline readability.

The only decorative system is the multi-stop mesh gradient (`{colors.gradient-develop-start}` `#007cf0` → `{colors.gradient-preview-end}` `#ff0080` → `{colors.gradient-ship-start}` `#ff4d4d` → `{colors.cyan}` `#50e3c2` / magenta / amber) that floats in atmospheric backdrops at hero scale. On dark backgrounds, gradient stop opacities are increased for visibility.

Type is the second decisive voice. The brand's own custom geometric sans (Geist) carries display, body, button — everything narrative — at weight 600 for display, 500 for buttons, 400 for body. A matching monospaced face (Geist Mono) carries technical labels: terminal mockups, code blocks, sometimes filename captions. Headlines are sentence-case with aggressive negative letter-spacing (`-2.4px` at 48 px hero) — the brand never letter-spaces positively, never goes uppercase outside of mono labels.

Surfaces use a three-step dark ladder: `{colors.canvas-soft}` (`#0f0f12` the page body), `{colors.canvas}` (`#18181b` card surfaces), `{colors.canvas-soft-2}` (`#1e1e22` occasional inset region). Shadows use white inset hairline rings (`0 0 0 1px rgba(255,255,255,0.06)`) instead of dark-on-light borders — cards glow faintly at their edges against the dark page.

**Key Characteristics:**
- Dark-only: `{colors.canvas-soft}` (`#0f0f12`) page background, `{colors.canvas}` (`#18181b`) card surfaces. No light mode, no toggle.
- Pink primary `{colors.primary}` (`#ec4899`) carries every CTA, active indicator, focus ring, and accent. Links use `{colors.link}` (`#f472b6`).
- A multi-stop mesh gradient (cyan-blue-magenta-amber) is the only decorative chrome — used at hero scale only. It is the brand.
- Every section eyebrow and small label uses the monospace face `{typography.caption-mono}` or `{typography.code}`; everything else is in the geometric sans.
- White inset hairline rings on dark cards replace dark-on-light borders — subtle glow instead of hard edges.
- A complete 100–1000 gray + blue + red + amber + green + teal + purple + pink color scale exists as a system token set, but the marketing surface uses only the dark-end tones.
- `pricing-card-featured` (Pro tier) uses `{colors.primary}` (`#ec4899` pink) against `{colors.canvas}` (`#18181b`) card siblings.
## Colors

### Brand & Accent
- **Primary** (`{colors.primary}` — `#ec4899`): The single primary CTA color. Vibrant pink that carries every button, active indicator, focus ring, and interactive accent on dark surfaces. WCAG AA on `#0f0f12` background.
- **Cyan** (`{colors.cyan}` — `#50e3c2`): A signature mint-cyan used in the brand gradient and inside Geist-system spotlight tokens. Visible inside the hero gradient stops.
- **Highlight Pink** (`{colors.highlight-pink}` — `#ff0080`): The brand's highlight magenta, used as the high-saturation stop in the preview-gradient pair.
- **Violet** (`{colors.violet}` — `#a78bfa`): A lighter violet tuned for dark backgrounds, used in the preview-gradient and accent highlights.
- **Link Pink** (`{colors.link}` — `#f472b6`): The brand's primary link color — pink-400, one step lighter than primary for inline readability on dark surfaces.
### Surface
- **Canvas** (`{colors.canvas}` — `#18181b`): The zinc-900 card / dialog / modal surface — one step lighter than the page background.
- **Canvas Soft** (`{colors.canvas-soft}` — `#0f0f12`): The default page background — near-black. Almost every section sits on this tone.
- **Canvas Soft 2** (`{colors.canvas-soft-2}` — `#1e1e22`): A slightly lighter inset surface for code editor inner backgrounds, template-card hover states, and dropdown menus.
- **Hairline** (`{colors.hairline}` — `#27272a`): 1 px dividers — table rows, card borders, input borders. Subtle on dark surfaces.
- **Hairline Strong** (`{colors.hairline-strong}` — `#3f3f46`): The zinc-700 gray, used as the slightly-stronger divider and deemphasized text color.
### Text
- **Ink** (`{colors.ink}` — `#e4e4e7`): Every heading and body paragraph on dark surfaces — zinc-200, near-white but not pure white to reduce eye strain.
- **Body** (`{colors.body}` — `#a1a1aa`): Secondary text — sub-headings, body captions, nav-link inactive text, footer column body.
- **Mute** (`{colors.mute}` — `#71717a`): Lowest-priority text — placeholder text, fine print, low-key labels.
- **On Primary** (`{colors.on-primary}` — `#ffffff`): All text on `{colors.primary}` (`#ec4899`) surfaces.
### Semantic
- **Success** (`{colors.success}` — `#4ade80`): The brand's success indicator — emerald-400, tuned for dark backgrounds.
- **Link Deep** (`{colors.link-deep}` — `#ec4899`): The pressed / visited tone for inline links — matches primary pink.
- **Link Bg Soft** (`{colors.link-bg-soft}` — `#ec489920`): Soft pink fill for "what's new" pill banners and informational badges.
- **Error** (`{colors.error}` — `#f87171`): Validation red for destructive actions and form errors — red-400 on dark.
- **Error Soft** (`{colors.error-soft}` — `#f8717120`): Soft red tint for destructive-state backgrounds.
- **Error Deep** (`{colors.error-deep}` — `#ef4444`): Pressed / deep destructive state — red-500.
- **Warning** (`{colors.warning}` — `#fbbf24`): Caution / pending status indicator — amber-400 on dark.
- **Warning Soft** (`{colors.warning-soft}` — `#fbbf2420`) / **Warning Deep** (`{colors.warning-deep}` — `#f59e0b`): Background + pressed variants.
### Brand Gradient
The brand's signature decoration is a three-pair gradient stack:
- **Develop** (`{colors.gradient-develop-start}` `#007cf0` → `{colors.gradient-develop-end}` `#00dfd8`) — the blue-to-teal pair used to mark the "deploy" / "develop" rhythm.
- **Preview** (`{colors.gradient-preview-start}` `#7928ca` → `{colors.gradient-preview-end}` `#ff0080`) — the violet-to-pink pair used for "preview" surfaces.
- **Ship** (`{colors.gradient-ship-start}` `#ff4d4d` → `{colors.gradient-ship-end}` `#f9cb28`) — the coral-to-amber pair used for "ship" surfaces.

The three pairs collapse into a single multi-color mesh gradient when used as the hero atmospheric backdrop. Treat the gradient as one unified object — do not crop down to a single color, do not reorder the stops, and do not miniaturize. Used at hero scale only.

## Typography

### Font Family
Two custom faces carry the entire system:

1. **A custom geometric sans** (extracted as `Geist`) for every display, body, button, link, and label. Weights 400 / 500 / 600 are the working set; the face never appears in 700 or heavier. Display sizes are tracked aggressively negative (`-2.4 px` at 48 px hero, `-1.28 px` at 32 px section); body stays at neutral or slightly-negative tracking.
2. **A custom monospaced face** (extracted as `Geist Mono`) for terminal mockups, code blocks, and small mono-caption labels — anything that wants to signal "technical." Weight 400 only at 12 – 13 px. Tracking neutral.

A condensed display sans (`Space Grotesk`) is loaded as a third face for occasional editorial moments but does not render as the primary face anywhere in the captured surfaces.

### Hierarchy

| Token | Size | Weight | Line Height | Letter Spacing | Use |
|---|---|---|---|---|---|
| `{typography.display-xl}` | 48px | 600 | 48px | -2.4px | Hero headline ("Build and deploy on the AI Cloud."). |
| `{typography.display-lg}` | 32px | 600 | 40px | -1.28px | Section headlines ("Your frontend, delivered.", "A compute model for all workloads."). |
| `{typography.display-md}` | 24px | 600 | 32px | -0.96px | Card-cluster headlines, pricing-tier names. |
| `{typography.display-sm}` | 20px | 600 | 28px | -0.6px | Inline display micro-headings. |
| `{typography.body-lg}` | 18px | 400 | 28px | 0 | Lead paragraphs under section headlines. |
| `{typography.body-md}` | 16px | 400 | 24px | 0 | Default body paragraph. |
| `{typography.body-md-strong}` | 16px | 500 | 24px | 0 | Bolded inline body. |
| `{typography.body-sm}` | 14px | 400 | 20px | -0.28px | Secondary body, nav-link text, button-md labels. |
| `{typography.body-sm-strong}` | 14px | 500 | 20px | -0.28px | Nav CTA labels, table-row emphasis. |
| `{typography.caption}` | 12px | 400 | 16px | 0 | Footer secondary lines, badge labels. |
| `{typography.caption-mono}` | 12px | 400 | 16px | 0 | Section eyebrows and label captions that want a technical voice. |
| `{typography.code}` | 13px | 400 | 20px | 0 | Inline code, terminal mockups, command snippets. |
| `{typography.button-md}` | 14px | 500 | 20px | 0 | Small / nav-scale button labels. |
| `{typography.button-lg}` | 16px | 500 | 24px | 0 | Marketing-scale pill button labels. |

### Principles
- **Negative tracking is part of the voice.** Display sizes use aggressive `-2.4` to `-0.6` px tracking. Reverting to default tracking breaks the brand.
- **Sentence-case headlines, period-terminated.** Headlines like "Build and deploy on the AI Cloud." end with a deliberate period — that punctuation is part of the brand's voice.
- **Mono for the technical layer only.** Section eyebrows, code blocks, terminal mockups. Body paragraphs never set in mono.
- **Weight 600 is the display ceiling.** The geometric sans never appears at 700 / 800. The brand reads as a calmer system because of this.

### Note on Font Substitutes
The two primary faces are proprietary (custom-cut for the brand). Open-source substitutes:
- **Geometric sans** — *Inter* (400 / 500 / 600) is the closest stylistic match; `font-feature-settings: "ss01", "ss02"` enables the geometric alternates. *Satoshi* is a passable second choice.
- **Monospace** — *JetBrains Mono* (400) at 12 – 13 px matches the technical voice. *IBM Plex Mono* is the second-best option.

## Layout

### Spacing System
- **Base unit**: 4 px. The brand's `--geist-space` token is exactly 4 px and every captured value is a multiple of 4.
- **Tokens**: `{spacing.xxs}` 4 px · `{spacing.xs}` 8 px · `{spacing.sm}` 12 px · `{spacing.md}` 16 px · `{spacing.lg}` 24 px · `{spacing.xl}` 32 px · `{spacing.2xl}` 40 px · `{spacing.3xl}` 48 px · `{spacing.4xl}` 64 px · `{spacing.5xl}` 96 px · `{spacing.6xl}` 128 px · `{spacing.section}` 192 px.
- **Section padding**: marketing bands use `{spacing.4xl}` to `{spacing.5xl}` top/bottom. Hero bands stretch to `{spacing.section}` to give the mesh gradient room to breathe.
- **Card interior padding**: marketing cards sit at `{spacing.lg}` to `{spacing.xl}`; template-grid cards stay tighter at `{spacing.md}` because they sit in a denser grid.
- **Inline gap**: button rows, nav rows, and chip rows use `{spacing.sm}` to `{spacing.md}` between siblings. The brand's `--geist-gap` is exactly 24 px.

### Grid & Container
- **Max width**: ~1400 px (`--ds-page-width`); the legacy `--geist-page-width` is 1200 px and still appears on some marketing surfaces. Content centers with horizontal gutters of `{spacing.lg}` 24 px on desktop, `{spacing.md}` 16 px on mobile.
- **Column patterns**:
  - Three-feature row: 3-up at desktop, 1-up at mobile (rows like "Web Apps / Composable Commerce / Multi-tenant Platforms").
  - Tab pill row: 5-up centered row of `tab-ghost` pills.
  - Template-grid cluster: 5-up at desktop, scaling to 1-up at mobile.
  - Pricing tier grid: 3-up at desktop with the middle tier polarity-flipped.
  - Logo strip: ~5 logos wide, single row.

### Whitespace Philosophy
The mesh gradient does most of the heavy decorative lifting; whitespace separates the bands. Section spacing is generous — `{spacing.4xl}` to `{spacing.5xl}` between bands lets the gradient breathe. Inside a card, the headline/paragraph stack is tight (`{spacing.xs}` 8 px gap), then a wider gap before the CTA cluster. The page reads as engineered — large gaps + tight interior, never the other way around.

### Responsive Strategy

#### Breakpoints

| Name | Width | Key Changes |
|---|---|---|
| Mobile | < 600px | Hero stacks; nav collapses to hamburger; 3-up feature grids drop to 1-up; tab pill row enables horizontal scroll. |
| Tablet | 600–959px | 3-up grids drop to 2-up; nav still horizontal. |
| Desktop | 960–1199px | Full 3-up grids; pricing 3-up. |
| Wide | 1200–1399px | Container caps at 1400 px content width. |
| Ultra-wide | ≥ 1400px | Content stays centered at 1400 px; bands stretch edge-to-edge in color but content holds the max-width. |

#### Touch Targets
The `button-primary` pill renders at ~32 px tall in nav and ~48 px tall in marketing contexts. Marketing CTAs comfortably meet WCAG AAA at all breakpoints; nav buttons inflate touch area through `{spacing.xs}` padding on mobile to meet the 44 × 44 px floor.

#### Collapsing Strategy
- **Nav**: full link row + Ask AI / Log In / Sign Up pills at desktop. Collapses to logo + hamburger at mobile with the menu opening as a full-overlay.
- **Hero**: mesh gradient stays centered; headline + body stack vertically at all breakpoints (the brand doesn't use a split-hero pattern).
- **Three-feature row**: 3-up → 2-up → 1-up at the breakpoints above; cards keep their `{rounded.md}` 8 px shape across all viewports.
- **Pricing card grid**: 3-up at desktop, vertical stack at mobile with `pricing-card-featured` always sitting in the middle.
- **Template grid**: 5-up → 3-up → 2-up → 1-up. Each `template-card` keeps its 16:9 aspect on the image.

#### Image Behavior
- **Mesh gradient**: rendered as inline SVG or canvas-painted gradient; scales fluidly with the hero container; never crops, never tiles.
- **Customer logos**: rendered as monochrome SVGs in the logo strip; consistent 24 px height.
- **Code editor mockup**: dark `{colors.primary}` (`#171717`) rectangle with mono text rendered inside; treated as an image at the layout level.
- **Template thumbnails**: 16:9 landscape inside `{rounded.md}` card chrome; lazy-loaded; consistent grayscale palette in the placeholder state.

## Elevation & Depth

| Level | Treatment | Use |
|---|---|---|
| Level 0 — Flat | No shadow, no border. | Full-bleed hero bands and dark sections. |
| Level 1 — Inset Hairline | `0 0 0 1px rgba(255,255,255,0.06)` inset 1 px border. | Default card chrome — white inset ring on dark surfaces. |
| Level 2 — Subtle Drop | `0px 1px 1px rgba(0,0,0,0.2), 0px 2px 2px rgba(0,0,0,0.15)` plus inset hairline. | Slightly elevated cards (template-grid, marketing-card). |
| Level 3 — Soft Stack | `0px 2px 2px rgba(0,0,0,0.2), 0px 8px 8px -8px rgba(0,0,0,0.15)` plus inset hairline. | The "medium" elevation — feature-grid cards. |
| Level 4 — Float Stack | `0px 2px 2px rgba(0,0,0,0.2), 0px 8px 16px -4px rgba(0,0,0,0.15)` plus inset hairline. | "Large" elevation — pricing cards, callout panels. |
| Level 5 — Modal | `0px 1px 1px rgba(0,0,0,0.2), 0px 8px 16px -4px rgba(0,0,0,0.15), 0px 24px 32px -8px rgba(0,0,0,0.2)` plus inset hairline. | Modal / dialog surfaces and dropdown menus. |

The brand uses STACKED shadows on dark backgrounds — multiple small offsets with white inset hairline rings (`rgba(255,255,255,0.06)`) so card edges glow subtly against the dark page.

### Decorative Depth
- **Mesh gradient as atmospheric depth**: the hero's multi-stop gradient is the brand's only "atmospheric" effect — applied as a flat 2-D backdrop rather than a 3-D illustration.
- **Dark surface hierarchy as section-depth**: switching between `{colors.canvas-soft}` (`#0f0f12`), `{colors.canvas}` (`#18181b`), and `{colors.canvas-soft-2}` (`#1e1e22`) creates subtle depth bands without needing polarity flips.
- **Inset-shadow + drop-shadow combo**: the cards' combination of an inset 1 px ring and a multi-stop drop produces a "card sits on the page" effect without ever feeling material-heavy.

## Shapes

### Border Radius Scale

| Token | Value | Use |
|---|---|---|
| `{rounded.none}` | 0px | Full-bleed hero / footer bands. |
| `{rounded.xs}` | 4px | Tightest inline pill — the `nav-cta-signup` 6-px-radius button (mapped to `xs/sm`). |
| `{rounded.sm}` | 6px | The brand's `--geist-radius` token — base UI radius for in-app buttons, form inputs, dropdown menus. |
| `{rounded.md}` | 8px | The brand's `--geist-marketing-radius` token — feature cards, template cards. |
| `{rounded.lg}` | 12px | Slightly larger card chrome (pricing-card variants). |
| `{rounded.xl}` | 16px | Largest card chrome — when a card hosts a hero image cap. |
| `{rounded.pill-sm}` | 64px | Tab-ghost pills inside the "AI Apps / Web Apps / Ecommerce / Marketing / Platforms" row. |
| `{rounded.pill}` | 100px | The marketing CTA pill — `button-primary`, `button-secondary`, "Start Deploying" pill. |
| `{rounded.full}` | 9999px | Icon-button circular containers, nav-link ghost pills. |

### Photography Geometry
- **Mesh gradient**: full-bleed 2-D atmospheric backdrop, never cropped to a frame; treated as the page's wallpaper.
- **Customer logos**: monochrome SVG, consistent 24 px height in a flex row.
- **Code editor mockup**: 16:10 dark rectangle, `{rounded.md}` corners.
- **Template thumbnails**: 16:9 landscape inside `{rounded.md}` chrome.
- **Showcase imagery**: 2:1 or 16:9 inside `{rounded.lg}` to `{rounded.xl}` chrome with a stacked shadow.

## Components

### Buttons

**`button-primary`** — the canonical 100-px-radius pink pill, marketing scale.
- Background `{colors.primary}` (`#ec4899`), text `{colors.on-primary}` (`#ffffff`), label set in `{typography.button-lg}`, padding `0px {spacing.sm}` 12 px, shape `{rounded.pill}` 100 px. Renders ~48 px tall when paired with the marketing flex layout.

**`button-secondary`** — the dark pill paired with the pink primary inside marketing bands.
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), same typography + padding as `button-primary`, shape `{rounded.pill}`.

**`button-primary-sm`** — the smaller-scale pink pill used inside nav and pricing-card CTAs.
- Background `{colors.primary}` (`#ec4899`), text `{colors.on-primary}` (`#ffffff`), label set in `{typography.button-md}` (14 px / 500), shape `{rounded.pill}`.

**`button-secondary-sm`** — the smaller-scale dark pill paired with `button-primary-sm`.
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), same typography + shape as `button-primary-sm`.

**`tab-ghost`** — the centered-row tab pill.
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), label set in `{typography.body-sm}`, padding `0px {spacing.md}`, shape `{rounded.pill-sm}` 64 px.

**`icon-button-circular`** — the circular icon container (often a "?" or arrow inside).
- Background `{colors.canvas}` (`#18181b`), light icon, 1 px solid `{colors.hairline}` (`#27272a`) border, shape `{rounded.full}`.

**Nav CTAs:**

**`nav-cta-signup`** — the small pink "Sign Up" button in the nav row.
- Background `{colors.primary}` (`#ec4899`), text `{colors.on-primary}` (`#ffffff`), label `{typography.body-sm-strong}`, padding `0px {spacing.xs}`, height 28 px, shape `{rounded.sm}` 6 px (the brand's `--geist-radius`).

**`nav-cta-login`** — the dark "Log In" button in the nav.
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), same typography / height / shape as `nav-cta-signup`.

**`nav-cta-ask-ai`** — the small "Ask AI" button with a faint border.
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), 1 px solid `{colors.hairline}` (`#27272a`) border, same typography / height / shape.

### Cards & Containers

**`card-marketing`** — the canonical marketing feature card (3-up section cards).
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), padding `{spacing.lg}` 24 px, shape `{rounded.md}` 8 px. Carries Level 3 soft-stack shadow with white inset hairline.

**`card-marketing-large`** — the larger marketing card used for "compute model" / "AI Gateway" callouts.
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), padding `{spacing.xl}`, shape `{rounded.lg}` 12 px. Carries Level 4 float-stack shadow.

**`card-soft`** — the soft-tinted card used inside cluster groups.
- Background `{colors.canvas-soft-2}` (`#1e1e22`), text `{colors.ink}` (`#e4e4e7`), padding `{spacing.lg}`, shape `{rounded.md}`.

**`template-card`** — the deploy-template card in the "Deploy your first app" grid.
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), padding `{spacing.md}` 16 px, shape `{rounded.md}` 8 px. Hosts a 16:9 thumbnail at the top.

**`code-editor-mockup`** — the deep-dark code-preview surface.
- Background `#0a0a0b`, text `{colors.on-primary}` (`#ffffff`), body in `{typography.code}` (13 px / Geist Mono), padding `{spacing.lg}` 24 px, shape `{rounded.md}` 8 px.

**`pricing-card`** — the default pricing-tier card.
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), padding `{spacing.xl}` 32 px, shape `{rounded.lg}` 12 px. Inside: tier name in `{typography.display-md}`, price in `{typography.display-xl}`, feature list in `{typography.body-md}` rows, CTA at the bottom.

**`pricing-card-featured`** — the pink-accented "Pro" tier card.
- Background `{colors.primary}` (`#ec4899`), text `{colors.on-primary}` (`#ffffff`), same shape + padding as `pricing-card`. CTA uses `button-secondary-sm` (dark pill on pink card).

### Inputs & Forms

**`form-input`** — the canonical text input.
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), 1 px solid `{colors.hairline}` (`#27272a`) border, body in `{typography.body-sm}` (14 px), padding `0px {spacing.sm}`, height 40 px (the brand's `--geist-form-height`), shape `{rounded.sm}` 6 px.

**`form-input-sm`** — small-height variant (32 px tall) for tight forms.
- Same as `form-input` but height 32 px (the `--geist-form-small-height`).

**`form-input-lg`** — large-height variant (48 px tall) for hero CTAs.
- Same as `form-input` but height 48 px (the `--geist-form-large-height`); body in `{typography.body-md}` 16 px.

### Navigation

**`nav-bar`** — the sticky top nav.
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), height 64 px (the brand's `--header-height`), padding `{spacing.sm} {spacing.lg}`. Layout: logo left, link row center, "Ask AI / Log In / Sign Up" cluster right.

**`nav-link`** — the centered link row inside `nav-bar`.
- Text `{colors.body}` (`#a1a1aa`), set in `{typography.body-sm}`, padding `{spacing.xs} {spacing.sm}`, shape `{rounded.full}` (ghost pill — visible only on hover or active, but the radius is documented).

**`footer`** — the bottom 4-column nav.
- Background `{colors.canvas-soft}` (`#0f0f12`), text `{colors.body}` (`#a1a1aa`), padding `{spacing.4xl} {spacing.lg}`. Eyebrow column labels in `{typography.caption-mono}` (uppercase mono effect); link rows in `{typography.body-sm}`.

### Signature Components

**`hero-band`** — the dark hero with the mesh gradient backdrop.
- Background `{colors.canvas-soft}` (`#0f0f12`), text `{colors.ink}` (`#e4e4e7`), padding `{spacing.4xl} {spacing.lg}`. Inside: a small mono badge above the headline, the headline in `{typography.display-xl}` (sentence-case, period-terminated), a body lead in `{typography.body-lg}`, then a CTA row with `button-primary` + `button-secondary`. The mesh gradient sits behind, scaled to occupy roughly the top half of the band.

**`feature-mesh-band`** — the secondary section that hosts a mesh-gradient atmospheric backdrop with feature copy on top.
- Background `{colors.canvas-soft}` (`#0f0f12`), text `{colors.ink}` (`#e4e4e7`), padding `{spacing.5xl} {spacing.lg}`. Section headline in `{typography.display-lg}`; supporting body in `{typography.body-md}`.

**`showcase-band-soft`** — a soft-dark section.
- Background `{colors.canvas}` (`#18181b`), text `{colors.ink}` (`#e4e4e7`), padding `{spacing.5xl} {spacing.lg}`.

**`showcase-band-deep`** — the deeper-dark band.
- Background `{colors.canvas-soft}` (`#0f0f12`), text `{colors.ink}` (`#e4e4e7`), padding `{spacing.5xl} {spacing.lg}`. Section headline in `{typography.display-lg}`. Often contains a `code-editor-mockup` flush with the band.

**`logo-strip`** — the customer-logo wrapping row near the top of the page.
- Background `{colors.canvas}` (`#18181b`), text `{colors.body}` (`#a1a1aa`), padding `{spacing.lg} {spacing.xl}`. Logos rendered as monochrome SVGs at consistent height.

**`badge-secondary`** — the small inline metadata pill ("New", "Beta", "Live").
- Background `{colors.canvas-soft-2}` (`#1e1e22`), text `{colors.body}` (`#a1a1aa`), body in `{typography.caption}`, padding `0px {spacing.xs}`, shape `{rounded.full}`.

**`banner-marketing`** — the "Introducing X" announcement pill at the top of pages.
- Background `{colors.canvas-soft-2}` (`#1e1e22`), text `{colors.body}` (`#a1a1aa`), body in `{typography.body-sm}`, padding `{spacing.xs} {spacing.sm}`, shape `{rounded.full}`.

**`link-inline`** — body-copy inline links.
- Text `{colors.link}` (`#f472b6`), body in `{typography.body-md}`, underlined.

### Examples (illustrative)

> Auto-derived kit-mirror demonstration surfaces (`scripts/derive-examples-block.mjs`). Each `ex-*` entry references brand-native primitives so downstream consumers (`/preview-design`, `/generate-kit`) re-skin the same 10 surfaces consistently. `TO_FILL` markers indicate missing primitives — resolve in the LLM judgment pass.

**`ex-pricing-tier`** — Default Pricing tier card. Re-uses feature-card chrome with brand canvas-soft surface.
- Properties: `backgroundColor`, `textColor`, `borderColor`, `rounded`, `padding`

**`ex-pricing-tier-featured`** — Featured/highlighted tier — polarity-flipped surface (dark fill + light text in light mode, light fill + dark text in dark mode).
- Properties: `backgroundColor`, `textColor`, `rounded`, `padding`

**`ex-product-selector`** — What's Included summary card — re-purposed for SaaS / B2B verticals (NOT a literal product gallery).
- Properties: `backgroundColor`, `rounded`, `padding`

**`ex-cart-drawer`** — Subscription summary — re-purposed for SaaS / B2B (line items per add-on, not literal cart).
- Properties: `backgroundColor`, `rounded`, `padding`, `item-divider`

**`ex-app-shell-row`** — Sidebar nav row inside the App Shell example. Active state uses brand primary as the indicator.
- Properties: `backgroundColor`, `activeIndicator`, `rounded`, `padding`

**`ex-data-table-cell`** — Default data-table th + td chrome. Header uses mono-caps eyebrow typography; body uses body-sm.
- Properties: `headerBackground`, `headerTypography`, `bodyTypography`, `cellPadding`, `rowBorder`

**`ex-auth-form-card`** — Sign-in / sign-up card. Re-uses feature-card chrome with text-input primitives inside.
- Properties: `backgroundColor`, `rounded`, `padding`

**`ex-modal-card`** — Modal dialog surface — same chrome as feature-card with elevated shadow.
- Properties: `backgroundColor`, `rounded`, `padding`

**`ex-empty-state-card`** — Empty-state illustration frame.
- Properties: `backgroundColor`, `rounded`, `padding`, `captionTypography`

**`ex-toast`** — Toast notification surface — feature-card shape + medium shadow.
- Properties: `backgroundColor`, `rounded`, `padding`, `typography`

## Known Gaps

- **Hover-state colors:** The brand uses subtle hover transitions on nav links and cards (typically a faint background fill shift toward `{colors.canvas-soft-2}` `#1e1e22`), but precise per-component hover tokens are not captured here.
- **Focus rings:** Form inputs use `{colors.primary}` (`#ec4899`) focus ring — the pink accent carries focus state on dark surfaces.
- **Loading skeletons:** Skeleton placeholders shimmer between `{colors.canvas-soft-2}` `#1e1e22` and `{colors.hairline}` `#27272a`, but the animation timing and stop tokens are not captured.
- **Dashboard / in-product surfaces:** This DESIGN.md covers the full dashboard. All surfaces use dark zinc tokens (`#0f0f12` / `#18181b` / `#1e1e22`) with Geist Mono vocabulary and tighter spacing.
- **Full gray scale:** The 100–1000 gray scale (and the parallel blue / red / amber / green / teal / purple / pink scales) exists as a system token set, but only the marketing-surface tones (`100`, `1000`, `700`-level) are documented; the in-product step values are not.
