/**
 * Typebot API Service
 * Per-instance Typebot integration settings and sessions.
 * All endpoints require the instance token in the apikey header.
 */

import apiClient from './client';

export type TypebotTriggerType = 'all' | 'keyword' | 'none';
export type TypebotTriggerOperator = 'equals' | 'contains' | 'startsWith';

export interface TypebotConfigInput {
  enabled: boolean;
  url: string;
  publicId: string;
  triggerType: TypebotTriggerType;
  triggerKeywords: string;
  triggerOperator: TypebotTriggerOperator;
  expireMinutes: number;
  keywordFinish: string;
  delayMessageMs: number;
  unknownMessage: string;
  stopBotFromMe: boolean;
  handoffChatwoot: boolean;
  ignoreJids: string;
}

export interface TypebotConfig extends TypebotConfigInput {
  instanceId: string;
  configured: boolean;
}

export interface TypebotSession {
  instanceId: string;
  remoteJid: string;
  sessionId: string;
  status: 'opened' | 'paused';
  createdAt: string;
  updatedAt: string;
}

/**
 * GET /typebot/config
 */
export const getTypebotConfig = async (instanceToken: string): Promise<TypebotConfig> => {
  const response = await apiClient.get<{ message: string; data: TypebotConfig }>('/typebot/config', {
    headers: { apikey: instanceToken },
  });
  return response.data.data;
};

/**
 * PUT /typebot/config
 * Checks that the bot is published on the viewer before saving.
 */
export const saveTypebotConfig = async (
  instanceToken: string,
  config: TypebotConfigInput
): Promise<TypebotConfig> => {
  const response = await apiClient.put<{ message: string; data: TypebotConfig }>('/typebot/config', config, {
    headers: { apikey: instanceToken },
  });
  return response.data.data;
};

/**
 * DELETE /typebot/config
 */
export const deleteTypebotConfig = async (instanceToken: string): Promise<void> => {
  await apiClient.delete('/typebot/config', { headers: { apikey: instanceToken } });
};

/**
 * GET /typebot/sessions
 */
export const getTypebotSessions = async (instanceToken: string): Promise<TypebotSession[]> => {
  const response = await apiClient.get<{ message: string; data: TypebotSession[] }>('/typebot/sessions', {
    headers: { apikey: instanceToken },
  });
  return response.data.data;
};

/**
 * DELETE /typebot/sessions
 * Ends every bot session of the instance.
 */
export const closeTypebotSessions = async (instanceToken: string): Promise<void> => {
  await apiClient.delete('/typebot/sessions', { headers: { apikey: instanceToken } });
};
