// White-label configuration. Values may be injected at serve time via a global
// `window.__ATRIUM__` object; anything absent falls back to the Atrium defaults.
// The accent colour is deliberately not here: it is injected server-side as a
// `<style>` :root override so it applies on first paint rather than via JS.

export interface BrandConfig {
  brandName: string
  brandSub: string
  defaultTheme: 'light' | 'dark'
}

// The server settings injected alongside the brand, mirroring webui.ShellConfig.
interface ShellConfig extends BrandConfig {
  maxUploadSize: number
}

declare global {
  interface Window {
    __ATRIUM__?: Partial<ShellConfig>
  }
}

const DEFAULTS: BrandConfig = {
  brandName: 'ATRIUM',
  brandSub: 'Secure Share',
  defaultTheme: 'light',
}

const { maxUploadSize: injectedMaxUploadSize, ...injectedBrand } =
  window.__ATRIUM__ ?? {}

export const brand: BrandConfig = { ...DEFAULTS, ...injectedBrand }

// The upload limit the server enforces, in bytes. Deliberately without a default:
// where nothing is injected (the Vite dev server) the client skips its own check
// and leaves the verdict to the server rather than inventing a limit.
export const maxUploadSize: number | undefined = injectedMaxUploadSize
