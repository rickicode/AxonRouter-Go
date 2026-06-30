/** @type {import('tailwindcss').Config} */
export default {
  content: ['./src/**/*.{html,js,svelte,ts}'],
  theme: {
    extend: {
      colors: {
        primary: '#ededed',
        'on-primary': '#000000',
        'accent-orange': '#fc4c02',
        'accent-magenta': '#ef2cc1',
        'accent-periwinkle': '#bdbbff',
        'accent-mint': '#c8f6f9',
        canvas: '#0a0a0a',
        hairline: '#222222',
        'canvas-dark': '#000000',
        'surface-dark-soft': '#111111',
        ink: '#ededed',
        body: '#888888',
        'on-dark': '#000000',
      },
      fontFamily: {
        display: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'Geist Mono', 'monospace'],
      },
      // ponytail: typography tokens removed — CSS classes in app.css are the source of truth.
      // All Svelte components use text-display-lg etc. from CSS, not Tailwind utilities.
      spacing: {
        'xxs': '4px',
        'xs': '8px',
        'sm': '12px',
        'md': '16px',
        'lg': '24px',
        'xl': '32px',
        '2xl': '40px',
        '3xl': '48px',
        '4xl': '64px',
        '5xl': '96px',
        '6xl': '128px',
        'section': '192px',
      },
      borderRadius: {
        'none': '0px',
        'xs': '4px',
        'sm': '6px',
        'md': '8px',
        'lg': '12px',
        'xl': '16px',
        'pill-sm': '64px',
        'pill': '100px',
        'full': '9999px',
      },
      boxShadow: {
        'vercel-1': '0 0 0 1px rgba(255,255,255,0.06) inset',
        'vercel-2': '0 0 0 1px rgba(255,255,255,0.06) inset, 0 1px 1px rgba(0,0,0,0.4), 0 2px 2px rgba(0,0,0,0.3)',
        'vercel-3': '0 0 0 1px rgba(255,255,255,0.06) inset, 0 2px 2px rgba(0,0,0,0.4), 0 8px 8px -8px rgba(0,0,0,0.5)',
        'vercel-4': '0 0 0 1px rgba(255,255,255,0.06) inset, 0 2px 2px rgba(0,0,0,0.5), 0 8px 16px -4px rgba(0,0,0,0.6)',
      },
      maxWidth: {
        'container': '1280px',
      },
      screens: {
        'mobile': '479px',
        'mobile-lg': '767px',
        'tablet': '991px',
        'desktop': '1279px',
        'desktop-lg': '1280px',
      },
    },
  },
  plugins: [],
}
