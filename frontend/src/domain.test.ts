import { describe, expect, it } from 'vitest'
import {
  SHARED_RESOURCE_BACKGROUND,
  libraryOptionSubtitle,
  sharedTagLabel,
  sortBooksForDisplay,
  togglePasswordVisibility,
  normalizePreferredLanguage,
  SUPPORTED_LANGUAGES,
  LANGUAGE_FLAGS,
  languageOptionText,
  commentDisplayLabel,
} from './domain'

describe('Bablex domain helpers', () => {
  it('uses a username for comment display and falls back to email', () => {
    expect(commentDisplayLabel('Ana', 'ana@example.com')).toBe('Ana')
    expect(commentDisplayLabel('', 'ana@example.com')).toBe('ana@example.com')
  })

  it('uses the requested shared resource background', () => {
    expect(SHARED_RESOURCE_BACKGROUND).toBe('#EEF2FF')
  })

  it('toggles password visibility', () => {
    expect(togglePasswordVisibility(false)).toBe(true)
    expect(togglePasswordVisibility(true)).toBe(false)
  })

  it('formats shared tags only when an email exists', () => {
    expect(sharedTagLabel(' owner@example.com ')).toBe('Shared: owner@example.com')
    expect(sharedTagLabel('')).toBe('')
  })

  it('orders owned books before shared books', () => {
    const books = [
      { id: 'shared', isShared: true },
      { id: 'owned-a', isShared: false },
      { id: 'owned-b', isShared: false },
    ]
    expect(sortBooksForDisplay(books).map((b) => b.id)).toEqual(['owned-a', 'owned-b', 'shared'])
  })

  it('formats the visible language selector with flag and acronym', () => {
    expect(LANGUAGE_FLAGS.en).toBe('🇬🇧')
    expect(languageOptionText('pt')).toBe('🇵🇹 PT')
  })

  it('supports the five application languages and normalizes invalid values', () => {
    expect(SUPPORTED_LANGUAGES).toEqual(['en', 'es', 'pt', 'fr', 'de'])
    expect(normalizePreferredLanguage('pt')).toBe('pt')
    expect(normalizePreferredLanguage('xx')).toBe('en')
    expect(normalizePreferredLanguage()).toBe('en')
  })

  it('shows shared owner only for shared libraries', () => {
    expect(libraryOptionSubtitle({ isShared: true, ownerEmail: 'owner@example.com' })).toBe('Shared: owner@example.com')
    expect(libraryOptionSubtitle({ isShared: false, ownerEmail: 'owner@example.com' })).toBe('')
  })
})
