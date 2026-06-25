import { afterEach, describe, expect, it, vi } from 'vitest';
import api from './client';
import { listBridgeAdapters } from './bridge';
import {
  createCronJob,
  deleteCronJob,
  listCronJobs,
  triggerCronJob,
  updateCronJob,
} from './cron';
import {
  getHeartbeat,
  pauseHeartbeat,
  resumeHeartbeat,
  setHeartbeatInterval,
  triggerHeartbeat,
} from './heartbeat';
import {
  addPlatformToProject,
  deleteProject,
  getProject,
  listAgentTypes,
  listProjects,
  updateProject,
} from './projects';
import {
  activateProvider,
  addGlobalProvider,
  addProvider,
  fetchProviderPresets,
  getProviderRefs,
  importCCSwitchProviders,
  listCCSwitchProviders,
  listGlobalProviders,
  listModels,
  listProviders,
  removeGlobalProvider,
  removeProvider,
  saveProviderRefs,
  setModel,
  updateGlobalProvider,
} from './providers';
import {
  createSession,
  deleteSession,
  getSession,
  listSessions,
  sendMessage,
  switchSession,
} from './sessions';
import { getGlobalSettings, updateGlobalSettings } from './settings';
import {
  setupFeishuBegin,
  setupFeishuPoll,
  setupFeishuSave,
} from './setup';
import { fetchSkillPresets, listSkills } from './skills';
import { getStatus, reloadConfig, restartSystem } from './status';

describe('api endpoint wrappers', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('maps project endpoints to the expected API calls', () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({});
    const post = vi.spyOn(api, 'post').mockResolvedValue({});
    const patch = vi.spyOn(api, 'patch').mockResolvedValue({});
    const del = vi.spyOn(api, 'delete').mockResolvedValue({});

    listAgentTypes();
    listProjects();
    getProject('demo');
    updateProject('demo', { language: 'en' });
    addPlatformToProject('demo', { type: 'feishu', options: { app_id: 'cli' } });
    deleteProject('demo');

    expect(get).toHaveBeenCalledWith('/agents');
    expect(get).toHaveBeenCalledWith('/projects');
    expect(get).toHaveBeenCalledWith('/projects/demo');
    expect(patch).toHaveBeenCalledWith('/projects/demo', { language: 'en' });
    expect(post).toHaveBeenCalledWith('/projects/demo/add-platform', {
      type: 'feishu',
      options: { app_id: 'cli' },
    });
    expect(del).toHaveBeenCalledWith('/projects/demo');
  });

  it('maps session endpoints including optional history limit', () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({});
    const post = vi.spyOn(api, 'post').mockResolvedValue({});
    const del = vi.spyOn(api, 'delete').mockResolvedValue({});

    listSessions('demo');
    getSession('demo', 'sid-1');
    getSession('demo', 'sid-2', 25);
    createSession('demo', { session_key: 'feishu:chat:user', name: 'Chat' });
    switchSession('demo', { session_key: 'feishu:chat:user', session_id: 'agent-sid' });
    sendMessage('demo', { session_key: 'feishu:chat:user', message: 'hello' });
    deleteSession('demo', 'sid-1');

    expect(get).toHaveBeenCalledWith('/projects/demo/sessions');
    expect(get).toHaveBeenCalledWith('/projects/demo/sessions/sid-1', undefined);
    expect(get).toHaveBeenCalledWith('/projects/demo/sessions/sid-2', { history_limit: '25' });
    expect(post).toHaveBeenCalledWith('/projects/demo/sessions', {
      session_key: 'feishu:chat:user',
      name: 'Chat',
    });
    expect(post).toHaveBeenCalledWith('/projects/demo/sessions/switch', {
      session_key: 'feishu:chat:user',
      session_id: 'agent-sid',
    });
    expect(post).toHaveBeenCalledWith('/projects/demo/send', {
      session_key: 'feishu:chat:user',
      message: 'hello',
    });
    expect(del).toHaveBeenCalledWith('/projects/demo/sessions/sid-1');
  });

  it('maps provider endpoints across project, global, presets, and cc-switch APIs', () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({});
    const post = vi.spyOn(api, 'post').mockResolvedValue({});
    const put = vi.spyOn(api, 'put').mockResolvedValue({});
    const del = vi.spyOn(api, 'delete').mockResolvedValue({});

    listProviders('demo');
    addProvider('demo', { name: 'p1' });
    removeProvider('demo', 'p1');
    activateProvider('demo', 'p2');
    listModels('demo');
    setModel('demo', 'gpt-test');
    getProviderRefs('demo');
    saveProviderRefs('demo', ['global-a']);
    listGlobalProviders();
    addGlobalProvider({ name: 'global-a' });
    updateGlobalProvider('global-a', { model: 'gpt-test' });
    removeGlobalProvider('global-a');
    fetchProviderPresets();
    listCCSwitchProviders();
    importCCSwitchProviders(['p1', 'p2']);

    expect(get).toHaveBeenCalledWith('/projects/demo/providers');
    expect(post).toHaveBeenCalledWith('/projects/demo/providers', { name: 'p1' });
    expect(del).toHaveBeenCalledWith('/projects/demo/providers/p1');
    expect(post).toHaveBeenCalledWith('/projects/demo/providers/p2/activate');
    expect(get).toHaveBeenCalledWith('/projects/demo/models');
    expect(post).toHaveBeenCalledWith('/projects/demo/model', { model: 'gpt-test' });
    expect(get).toHaveBeenCalledWith('/projects/demo/provider-refs');
    expect(put).toHaveBeenCalledWith('/projects/demo/provider-refs', { provider_refs: ['global-a'] });
    expect(get).toHaveBeenCalledWith('/providers');
    expect(post).toHaveBeenCalledWith('/providers', { name: 'global-a' });
    expect(put).toHaveBeenCalledWith('/providers/global-a', { model: 'gpt-test' });
    expect(del).toHaveBeenCalledWith('/providers/global-a');
    expect(get).toHaveBeenCalledWith('/providers/presets');
    expect(get).toHaveBeenCalledWith('/providers/cc-switch');
    expect(post).toHaveBeenCalledWith('/providers/cc-switch', { names: ['p1', 'p2'] });
  });

  it('maps cron, heartbeat, setup, skills, bridge, settings, and status endpoints', () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({});
    const post = vi.spyOn(api, 'post').mockResolvedValue({});
    const patch = vi.spyOn(api, 'patch').mockResolvedValue({});
    const del = vi.spyOn(api, 'delete').mockResolvedValue({});

    listCronJobs();
    listCronJobs('demo');
    createCronJob({ project: 'demo', prompt: 'daily' });
    updateCronJob('cron-1', { enabled: false });
    deleteCronJob('cron-1');
    triggerCronJob('cron-1');

    getHeartbeat('demo');
    pauseHeartbeat('demo');
    resumeHeartbeat('demo');
    triggerHeartbeat('demo');
    setHeartbeatInterval('demo', 15);

    setupFeishuBegin();
    setupFeishuPoll('device-1', 'https://accounts.feishu.cn');
    setupFeishuSave({ project: 'demo', app_id: 'cli', app_secret: 'sec', platform_type: 'feishu' });

    listSkills();
    fetchSkillPresets();
    listBridgeAdapters();
    getGlobalSettings();
    updateGlobalSettings({ language: 'zh' });
    getStatus();
    restartSystem({ session_key: 'feishu:chat:user', platform: 'feishu' });
    reloadConfig();

    expect(get).toHaveBeenCalledWith('/cron', undefined);
    expect(get).toHaveBeenCalledWith('/cron', { project: 'demo' });
    expect(post).toHaveBeenCalledWith('/cron', { project: 'demo', prompt: 'daily' });
    expect(patch).toHaveBeenCalledWith('/cron/cron-1', { enabled: false });
    expect(del).toHaveBeenCalledWith('/cron/cron-1');
    expect(post).toHaveBeenCalledWith('/cron/cron-1/exec');

    expect(get).toHaveBeenCalledWith('/projects/demo/heartbeat');
    expect(post).toHaveBeenCalledWith('/projects/demo/heartbeat/pause');
    expect(post).toHaveBeenCalledWith('/projects/demo/heartbeat/resume');
    expect(post).toHaveBeenCalledWith('/projects/demo/heartbeat/run');
    expect(post).toHaveBeenCalledWith('/projects/demo/heartbeat/interval', { minutes: 15 });

    expect(post).toHaveBeenCalledWith('/setup/feishu/begin', {});
    expect(post).toHaveBeenCalledWith('/setup/feishu/poll', {
      device_code: 'device-1',
      base_url: 'https://accounts.feishu.cn',
    });
    expect(post).toHaveBeenCalledWith('/setup/feishu/save', {
      project: 'demo',
      app_id: 'cli',
      app_secret: 'sec',
      platform_type: 'feishu',
    });

    expect(get).toHaveBeenCalledWith('/skills');
    expect(get).toHaveBeenCalledWith('/skills/presets');
    expect(get).toHaveBeenCalledWith('/bridge/adapters');
    expect(get).toHaveBeenCalledWith('/settings');
    expect(patch).toHaveBeenCalledWith('/settings', { language: 'zh' });
    expect(get).toHaveBeenCalledWith('/status');
    expect(post).toHaveBeenCalledWith('/restart', { session_key: 'feishu:chat:user', platform: 'feishu' });
    expect(post).toHaveBeenCalledWith('/reload');
  });
});
