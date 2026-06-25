import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { api } from '@/api/client';
import { useAuthStore } from './auth';

describe('auth store', () => {
  beforeEach(() => {
    localStorage.clear();
    api.setToken('');
    useAuthStore.setState({ token: '', serverUrl: '', isAuthenticated: false });
  });

  afterEach(() => {
    localStorage.clear();
  });

  it('persists login token and optional server URL', () => {
    useAuthStore.getState().login('token-1', 'https://cc.example');

    expect(useAuthStore.getState()).toMatchObject({
      token: 'token-1',
      serverUrl: 'https://cc.example',
      isAuthenticated: true,
    });
    expect(api.getToken()).toBe('token-1');
    expect(localStorage.getItem('cc_token')).toBe('token-1');
    expect(localStorage.getItem('cc_server_url')).toBe('https://cc.example');
  });

  it('clears token, server URL, and API auth on logout', () => {
    useAuthStore.getState().login('token-2', 'https://cc.example');

    useAuthStore.getState().logout();

    expect(useAuthStore.getState()).toMatchObject({
      token: '',
      serverUrl: '',
      isAuthenticated: false,
    });
    expect(api.getToken()).toBe('');
    expect(localStorage.getItem('cc_token')).toBeNull();
    expect(localStorage.getItem('cc_server_url')).toBeNull();
  });

  it('restores an authenticated session from localStorage', () => {
    localStorage.setItem('cc_token', 'saved-token');
    localStorage.setItem('cc_server_url', 'https://saved.example');

    useAuthStore.getState().init();

    expect(useAuthStore.getState()).toMatchObject({
      token: 'saved-token',
      serverUrl: 'https://saved.example',
      isAuthenticated: true,
    });
    expect(api.getToken()).toBe('saved-token');
  });

  it('keeps logged-out state when no saved token exists', () => {
    localStorage.setItem('cc_server_url', 'https://saved.example');

    useAuthStore.getState().init();

    expect(useAuthStore.getState()).toMatchObject({
      token: '',
      serverUrl: '',
      isAuthenticated: false,
    });
    expect(api.getToken()).toBe('');
  });
});
