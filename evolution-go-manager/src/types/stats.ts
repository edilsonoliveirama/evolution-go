export interface DashboardStats {
  instances: {
    total: number;
    connected: number;
    disconnected: number;
  };
  messages: {
    sent: number;
    received: number;
  };
  uptimeSeconds: number;
}

export interface RawDashboardStatsResponse {
  message: string;
  data: DashboardStats;
}
