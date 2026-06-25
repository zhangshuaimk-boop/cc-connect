import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import api, { ApiError } from './client';

const jsonResponse = (body: unknown, init?: ResponseInit) =>
  new Response(JSON.stringify(body), {
    status: init?.status ?? 200,
    statusText: init?.statusText,
    headers: { 'Content-Type': 'application/json' },
  });

describe('api client', () => {
  beforeEach(() => {
    api.setToken('');
    vi.stubGlobal('fetch', vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('sends bearer auth, JSON body, and query params', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse({ ok: true, data: { saved: true } }));
    api.setToken('token-123');

    const got = await api.post('/projects/demo', { enabled: true });

    expect(got).toEqual({ saved: true });
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/projects/demo', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer token-123',
      },
      body: JSON.stringify({ enabled: true }),
    });

    fetchMock.mockResolvedValueOnce(jsonResponse({ ok: true, data: ['a'] }));
    await api.get('/sessions', { project: 'demo one', limit: '10' });

    expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/sessions?project=demo+one&limit=10', {
      method: 'GET',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer token-123',
      },
      body: undefined,
    });
  });

  it('throws ApiError with server message for failed JSON envelopes', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse({ ok: false, error: 'bad request' }, { status: 400 }));

    await expect(api.get('/broken')).rejects.toMatchObject({
      name: 'ApiError',
      message: 'bad request',
      status: 400,
    });
  });

  it('calls the unauthorized handler and skips parsing response body on 401', async () => {
    const onUnauthorized = vi.fn();
    api.setOnUnauthorized(onUnauthorized);
    vi.mocked(fetch).mockResolvedValueOnce(new Response('', { status: 401 }));

    await expect(api.get('/private')).rejects.toBeInstanceOf(ApiError);

    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it('fetches raw text without forcing JSON content type', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(new Response('plain config', { status: 200, statusText: 'OK' }));
    api.setToken('raw-token');

    await expect(api.raw('/config/raw')).resolves.toBe('plain config');

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/config/raw', {
      headers: { Authorization: 'Bearer raw-token' },
    });
  });

  it('turns unsuccessful raw responses into ApiError', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('missing', { status: 404, statusText: 'Not Found' }));

    await expect(api.raw('/missing')).rejects.toMatchObject({
      name: 'ApiError',
      message: 'Not Found',
      status: 404,
    });
  });
});
