/**
 * Chatwoot API Service
 * Per-instance Chatwoot integration settings.
 * All endpoints require the instance token in the apikey header.
 */

import apiClient from './client';

export interface ChatwootConfigInput {
  enabled: boolean;
  url: string;
  accountId: number;
  token: string;
  inboxName: string;
  autoCreateInbox: boolean;
  webhookBaseUrl: string;
  signMessages: boolean;
  signDelimiter: string;
  reopenConversation: boolean;
  conversationPending: boolean;
  mergeBrazilContacts: boolean;
  ignoreJids: string;
  importContacts: boolean;
  importMessages: boolean;
  daysLimitImport: number;
}

export interface ChatwootConfig extends ChatwootConfigInput {
  instanceId: string;
  inboxId: number;
  inboxIdentifier: string;
  webhookUrl: string;
  importAvailable: boolean;
  configured: boolean;
}

/**
 * GET /chatwoot/config
 */
export const getChatwootConfig = async (
  instanceToken: string
): Promise<ChatwootConfig> => {
  const response = await apiClient.get<{ message: string; data: ChatwootConfig }>(
    '/chatwoot/config',
    { headers: { apikey: instanceToken } }
  );
  return response.data.data;
};

/**
 * PUT /chatwoot/config
 * Validates the Chatwoot credentials, finds/creates the API inbox and sets its webhook.
 */
export const saveChatwootConfig = async (
  instanceToken: string,
  config: ChatwootConfigInput
): Promise<ChatwootConfig> => {
  const response = await apiClient.put<{ message: string; data: ChatwootConfig }>(
    '/chatwoot/config',
    config,
    { headers: { apikey: instanceToken }, timeout: 60000 }
  );
  return response.data.data;
};

/**
 * DELETE /chatwoot/config
 */
export const deleteChatwootConfig = async (instanceToken: string): Promise<void> => {
  await apiClient.delete('/chatwoot/config', {
    headers: { apikey: instanceToken },
  });
};

/**
 * POST /chatwoot/import-contacts
 * Starts importing the WhatsApp address book into Chatwoot (background).
 */
export const importChatwootContacts = async (instanceToken: string): Promise<void> => {
  await apiClient.post('/chatwoot/import-contacts', null, {
    headers: { apikey: instanceToken },
  });
};
