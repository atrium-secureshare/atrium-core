// White-label configuration. Values may be injected at serve time via a global
// `window.__ATRIUM__` object; anything absent falls back to the Atrium defaults.
// The accent colour is deliberately not here: it is injected server-side as a
// `<style>` :root override so it applies on first paint rather than via JS.

export interface BrandConfig {
  brandName: string
  brandSub: string
  defaultTheme: 'light' | 'dark'
}

declare global {
  interface Window {
    __ATRIUM__?: Partial<BrandConfig>
  }
}

const DEFAULTS: BrandConfig = {
  brandName: 'ATRIUM',
  brandSub: 'Secure Share',
  defaultTheme: 'light',
}

export const brand: BrandConfig = { ...DEFAULTS, ...(window.__ATRIUM__ ?? {}) }
