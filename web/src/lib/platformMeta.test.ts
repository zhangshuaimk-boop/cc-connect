import { describe, expect, it } from 'vitest';
import { platformMeta } from './platformMeta';

describe('platformMeta', () => {
  it('exposes Feishu/Lark as the only platform setup metadata', () => {
    expect(Object.keys(platformMeta)).toEqual(['feishu']);
    expect(platformMeta.feishu.label).toBe('Feishu / Lark');
  });

  it('marks Feishu credentials as required and allow_from as advanced optional config', () => {
    expect(platformMeta.feishu.fields).toEqual([
      { key: 'app_id', labelKey: 'fields.appId', required: true },
      { key: 'app_secret', labelKey: 'fields.appSecret', required: true, type: 'password' },
      { key: 'allow_from', labelKey: 'fields.allowFrom', placeholder: '* (all)', group: 'advanced' },
    ]);
  });
});
