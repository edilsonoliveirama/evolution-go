/**
 * Monitoring API Service
 * Message volume, instance health and alert settings.
 * All endpoints require the global API key (admin).
 */

import apiClient from './client';

export interface MonitoringTotals {
  messagesIn: number;
  messagesOut: number;
  newContacts: number;
  activeChats: number;
  delivered: number;
  read: number;
  failures: number;
  disconnects: number;
  loggedOuts: number;
  bans: number;
  connected: number;
  instanceCount: number;
}

export interface DayPoint {
  day: string;
  in: number;
  out: number;
  newContacts: number;
  activeChats: number;
  failures: number;
}

export interface HourPoint {
  hour: string;
  in: number;
  out: number;
}

export interface InstanceHealth {
  id: string;
  name: string;
  connected: boolean;
  downSince?: string;
  uptimePercent: number | null;
  disconnects: number;
  loggedOuts: number;
  bans: number;
  failures: number;
  messagesIn: number;
  messagesOut: number;
}

export interface MonitoringDashboard {
  days: number;
  from: string;
  to: string;
  timezone: string;
  retentionDays: number;
  totals: MonitoringTotals;
  daily: DayPoint[];
  hourly?: HourPoint[];
  types: { type: string; in: number; out: number }[] | null;
  failures: { component: string; count: number }[] | null;
  heatmap: { dow: number; hour: number; count: number }[] | null;
  instances: InstanceHealth[] | null;
}

export interface MonitoringEvent {
  id: number;
  instanceId: string;
  instanceName: string;
  kind: string;
  detail: string;
  createdAt: string;
}

export interface AlertConfig {
  enabled: boolean;
  notifierInstanceId: string;
  numbers: string;
  webhookUrl: string;
  disconnectMinutes: number;
  failureThreshold: number;
  updatedAt?: string;
}

/**
 * GET /monitoring/dashboard
 */
export const getDashboard = async (days: number, instanceId?: string): Promise<MonitoringDashboard> => {
  const response = await apiClient.get<{ message: string; data: MonitoringDashboard }>('/monitoring/dashboard', {
    params: { days, instanceId: instanceId || undefined },
  });
  return response.data.data;
};

/**
 * GET /monitoring/events
 */
export const getEvents = async (instanceId?: string, limit = 50): Promise<MonitoringEvent[]> => {
  const response = await apiClient.get<{ message: string; data: MonitoringEvent[] }>('/monitoring/events', {
    params: { instanceId: instanceId || undefined, limit },
  });
  return response.data.data || [];
};

/**
 * GET /monitoring/alerts
 */
export const getAlertConfig = async (): Promise<AlertConfig> => {
  const response = await apiClient.get<{ message: string; data: AlertConfig }>('/monitoring/alerts');
  return response.data.data;
};

/**
 * PUT /monitoring/alerts
 */
export const saveAlertConfig = async (config: AlertConfig): Promise<AlertConfig> => {
  const response = await apiClient.put<{ message: string; data: AlertConfig }>('/monitoring/alerts', config);
  return response.data.data;
};

/**
 * POST /monitoring/alerts/test
 * Sends a test message to every configured channel.
 */
export const sendTestAlert = async (): Promise<void> => {
  await apiClient.post('/monitoring/alerts/test');
};
