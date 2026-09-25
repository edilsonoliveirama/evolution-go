/**
 * Dashboard Stats API Service
 * Handles the Evolution GO aggregated stats endpoint
 */

import apiClient from './client';
import type { DashboardStats, RawDashboardStatsResponse } from '@/types/stats';

/**
 * Fetch aggregated dashboard stats
 * GET /instance/stats
 */
export const fetchDashboardStats = async (): Promise<DashboardStats> => {
  const response = await apiClient.get<RawDashboardStatsResponse>('/instance/stats');
  return response.data.data;
};
