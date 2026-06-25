import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useThemeStore } from './theme';

function stubPrefersDark(matches: boolean) {
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({
    matches,
    media: '(prefers-color-scheme: dark)',
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
}

describe('theme store', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.className = '';
    useThemeStore.setState({ theme: 'dark', resolved: 'dark' });
    vi.unstubAllGlobals();
  });

  it('applies explicit light and dark themes', () => {
    useThemeStore.getState().setTheme('light');

    expect(useThemeStore.getState()).toMatchObject({ theme: 'light', resolved: 'light' });
    expect(localStorage.getItem('cc_theme')).toBe('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);

    useThemeStore.getState().setTheme('dark');

    expect(useThemeStore.getState()).toMatchObject({ theme: 'dark', resolved: 'dark' });
    expect(localStorage.getItem('cc_theme')).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('resolves system theme from matchMedia', () => {
    stubPrefersDark(true);

    useThemeStore.getState().setTheme('system');

    expect(useThemeStore.getState()).toMatchObject({ theme: 'system', resolved: 'dark' });
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('initializes from saved theme and defaults to dark', () => {
    localStorage.setItem('cc_theme', 'light');

    useThemeStore.getState().init();

    expect(useThemeStore.getState()).toMatchObject({ theme: 'light', resolved: 'light' });
    expect(document.documentElement.classList.contains('dark')).toBe(false);

    localStorage.clear();
    useThemeStore.getState().init();

    expect(useThemeStore.getState()).toMatchObject({ theme: 'dark', resolved: 'dark' });
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });
});
