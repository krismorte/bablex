export type DisplayBook = { isShared?: boolean }

export const SHARED_RESOURCE_BACKGROUND = '#EEF2FF'

export function togglePasswordVisibility(current: boolean): boolean {
  return !current
}

export function sharedTagLabel(email?: string): string {
  const normalized = (email || '').trim()
  return normalized ? `Shared: ${normalized}` : ''
}

export function sortBooksForDisplay<T extends DisplayBook>(books: T[]): T[] {
  return [...books].sort((a, b) => Number(Boolean(a.isShared)) - Number(Boolean(b.isShared)))
}

export function libraryOptionSubtitle(library: { isShared?: boolean; ownerEmail?: string }): string {
  return library.isShared && library.ownerEmail ? `Shared: ${library.ownerEmail}` : ''
}

export const SUPPORTED_LANGUAGES = ['en', 'es', 'pt', 'fr', 'de'] as const
export type SupportedLanguage = typeof SUPPORTED_LANGUAGES[number]
export function normalizePreferredLanguage(value?: string): SupportedLanguage {
  return SUPPORTED_LANGUAGES.includes(value as SupportedLanguage) ? value as SupportedLanguage : 'en'
}

export const LANGUAGE_FLAGS: Record<SupportedLanguage, string> = { en: '🇬🇧', es: '🇪🇸', pt: '🇵🇹', fr: '🇫🇷', de: '🇩🇪' }
export function languageOptionText(language: SupportedLanguage): string {
  return `${LANGUAGE_FLAGS[language]} ${language.toUpperCase()}`
}

export const commentDisplayLabel = (userName: string, userEmail: string): string => (userName || userEmail).trim()
