/** Palette: burgundy #8C2B32 and olive #5D5C31 on near-black, from the approved reference. */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: '#0B0A0A', card: '#141212', raised: '#1B1818', line: '#241F1F',
        accent: { DEFAULT: '#8C2B32', hover: '#A33740' },
        olive: { DEFAULT: '#5D5C31', light: '#8C8B4A' },
        ink: { DEFAULT: '#EDEAEA', mute: '#9A9494' },
        warn: '#B8893C', danger: '#C4505A',
      },
      fontFamily: { sans: ['"Inter Variable"', 'system-ui', 'sans-serif'], mono: ['"JetBrains Mono"', 'ui-monospace', 'monospace'] },
      borderRadius: { xl: '14px', '2xl': '18px' },
    },
  },
  plugins: [],
}
