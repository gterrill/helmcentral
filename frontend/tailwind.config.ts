import type { Config } from 'tailwindcss'

const config: Config = {
  darkMode: ['class'],
  content: ['./index.html', './src/**/*.{ts,tsx}', './.design-sync/previews/**/*.{ts,tsx}'],
  theme: {
  	extend: {
  		colors: {
  			background: 'hsl(var(--background))',
  			foreground: 'hsl(var(--foreground))',
  			card: 'hsl(var(--card))',
  			'card-foreground': 'hsl(var(--card-foreground))',
  			popover: 'hsl(var(--popover))',
  			'popover-foreground': 'hsl(var(--popover-foreground))',
  			primary: 'hsl(var(--primary))',
  			'primary-foreground': 'hsl(var(--primary-foreground))',
  			secondary: 'hsl(var(--secondary))',
  			'secondary-foreground': 'hsl(var(--secondary-foreground))',
  			muted: 'hsl(var(--muted))',
  			'muted-foreground': 'hsl(var(--muted-foreground))',
  			accent: 'hsl(var(--accent))',
  			'accent-foreground': 'hsl(var(--accent-foreground))',
  			border: 'hsl(var(--border))',
  			input: 'hsl(var(--input))',
  			ring: 'hsl(var(--ring))',
  			destructive: 'hsl(var(--destructive))',
  			'destructive-foreground': 'hsl(var(--destructive-foreground))',
  			'gauge-primary': 'hsl(var(--gauge-primary))',
  			'gauge-primary-label': 'hsl(var(--gauge-primary-label))',
  			'gauge-secondary': 'hsl(var(--gauge-secondary))',
  			'chart-precip': 'hsl(var(--chart-precip))',
  			'chart-uv': 'hsl(var(--chart-uv))',
  			'chart-gust': 'hsl(var(--chart-gust))',
  			sidebar: {
  				DEFAULT: 'hsl(var(--sidebar-background))',
  				foreground: 'hsl(var(--sidebar-foreground))',
  				primary: 'hsl(var(--sidebar-primary))',
  				'primary-foreground': 'hsl(var(--sidebar-primary-foreground))',
  				accent: 'hsl(var(--sidebar-accent))',
  				'accent-foreground': 'hsl(var(--sidebar-accent-foreground))',
  				border: 'hsl(var(--sidebar-border))',
  				ring: 'hsl(var(--sidebar-ring))'
  			}
  		},
  		borderRadius: {
  			lg: 'var(--radius)',
  			md: 'calc(var(--radius) - 2px)',
  			sm: 'calc(var(--radius) - 4px)',
  			xl: 'calc(var(--radius) * 2)',
  			'2xl': 'calc(var(--radius) * 3)'
  		},
  		fontSize: {
  			'2xs': ['0.625rem', { lineHeight: '0.875rem' }]
  		},
  		fontFamily: {
  			display: [
  				'var(--font-display)',
  				'monospace'
  			],
  			sans: [
  				'var(--font-sans)',
  				'sans-serif'
  			]
  		}
  	}
  },
  plugins: [],
}

export default config
