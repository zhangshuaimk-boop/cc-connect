export interface FieldDef {
  key: string;
  labelKey: string;
  required?: boolean;
  type?: 'text' | 'password' | 'number' | 'boolean';
  placeholder?: string;
  hintKey?: string;
  group?: 'basic' | 'advanced';
}

export interface PlatformMeta {
  label: string;
  fields: FieldDef[];
}

export const platformMeta: Record<string, PlatformMeta> = {
  feishu: {
    label: 'Feishu / Lark',
    fields: [
      { key: 'app_id', labelKey: 'fields.appId', required: true },
      { key: 'app_secret', labelKey: 'fields.appSecret', required: true, type: 'password' },
      { key: 'allow_from', labelKey: 'fields.allowFrom', placeholder: '* (all)', group: 'advanced' },
    ],
  },
};
