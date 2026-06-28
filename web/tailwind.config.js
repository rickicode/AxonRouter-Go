/** @type {import('tailwindcss').Config} */
export default {
  content: ['./src/**/*.{html,js,svelte,ts}'],
  theme: {
    extend: {
      // Color System from DESIGN.md
      colors: {
        // Brand & Accent
        primary: '#000000',
        'accent-orange': '#fc4c02',
        'accent-magenta': '#ef2cc1',
        'accent-periwinkle': '#bdbbff',
        'accent-mint': '#c8f6f9',
        
        // Surface
        canvas: '#ffffff',
        hairline: '#ebebeb',
        'canvas-dark': '#010120',
        'surface-dark-soft': '#26263a',
        
        // Text
        ink: '#000000',
        body: '#999999',
        'on-dark': '#ffffff',
        'on-primary': '#ffffff',
      },
      
      // Typography from DESIGN.md
      fontFamily: {
        display: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'Geist Mono', 'monospace'],
      },
      
      fontSize: {
        'display-xxl': ['64px', { lineHeight: '70.4px', letterSpacing: '-1.92px' }],
        'display-xl': ['40px', { lineHeight: '48px', letterSpacing: '-0.8px' }],
        'display-lg': ['28px', { lineHeight: '32.2px', letterSpacing: '-0.42px' }],
        'display-md': ['22px', { lineHeight: '25.3px', letterSpacing: '-0.22px' }],
        'body-lg': ['18px', { lineHeight: '23.4px', letterSpacing: '-0.18px' }],
        'body-md': ['16px', { lineHeight: '20.8px', letterSpacing: '-0.16px' }],
        'caption': ['14px', { lineHeight: '19.6px', letterSpacing: '0' }],
        'mono-caps-button': ['16px', { lineHeight: '16px', letterSpacing: '0.08px' }],
        'mono-caps-eyebrow': ['11px', { lineHeight: '11px', letterSpacing: '0.55px' }],
        'mono-caps-label': ['11px', { lineHeight: '15.4px', letterSpacing: '0.055px' }],
        'mono-caption': ['10px', { lineHeight: '14px', letterSpacing: '0.05px' }],
      },
      
      // Spacing System (4px base)
      spacing: {
        'xxs': '2px',
        'xs': '4px',
        'sm': '8px',
        'md': '12px',
        'lg': '16px',
        'xl': '20px',
        '2xl': '24px',
        '3xl': '32px',
        '4xl': '44px',
        '5xl': '48px',
        '6xl': '55.2px',
        'section': '80px',
      },
      
      // Border Radius
      borderRadius: {
        'none': '0px',
        'xs': '3.25px',
        'sm': '4px',
        'md': '8px',
        'full': '9999px',
      },
      
      // Box Shadow
      boxShadow: {
        'soft-drop': '0px 4px 10px 0px rgba(1, 1, 32, 0.1)',
      },
      
      // Max Width
      maxWidth: {
        'container': '1280px',
      },
      
      // Breakpoints
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
