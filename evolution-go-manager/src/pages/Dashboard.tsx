import { useEffect, useRef, useState } from 'react';
import { Card, CardContent, Skeleton } from '@evoapi/design-system';
import { Smartphone, Send, Inbox, Clock } from 'lucide-react';
import { toast } from 'sonner';
import { fetchDashboardStats } from '@/services/api/stats';
import type { DashboardStats } from '@/types/stats';

const POLL_INTERVAL_MS = 10000;

function formatUptime(totalSeconds: number): string {
  const s = totalSeconds % 60;
  const totalMinutes = Math.floor(totalSeconds / 60);
  const m = totalMinutes % 60;
  const totalHours = Math.floor(totalMinutes / 60);
  const h = totalHours % 24;
  const d = Math.floor(totalHours / 24);

  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

interface StatCardProps {
  label: string;
  value: string;
  sub?: string;
  icon: React.ElementType;
}

function StatCard({ label, value, sub, icon: Icon }: StatCardProps) {
  return (
    <Card className="bg-sidebar border-sidebar-border">
      <CardContent className="p-5">
        <div className="flex items-center justify-between mb-3">
          <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            {label}
          </span>
          <Icon className="h-4 w-4 text-muted-foreground" />
        </div>
        <div className="text-3xl font-bold tabular-nums text-foreground">{value}</div>
        {sub && <div className="mt-1 text-xs text-muted-foreground">{sub}</div>}
      </CardContent>
    </Card>
  );
}

function StatCardSkeleton() {
  return (
    <Card className="bg-sidebar border-sidebar-border">
      <CardContent className="p-5">
        <Skeleton className="h-4 w-24 mb-4" />
        <Skeleton className="h-8 w-16" />
      </CardContent>
    </Card>
  );
}

export default function Dashboard() {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const hasShownError = useRef(false);

  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      try {
        const data = await fetchDashboardStats();
        if (!cancelled) {
          setStats(data);
          hasShownError.current = false;
        }
      } catch (error) {
        if (!cancelled && !hasShownError.current) {
          const message =
            error && typeof error === 'object' && 'message' in error
              ? String((error as { message: unknown }).message)
              : 'Não foi possível carregar as métricas.';
          toast.error(message);
          hasShownError.current = true;
        }
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    };

    load();
    const interval = setInterval(load, POLL_INTERVAL_MS);

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  return (
    <div className="p-6">
      <h1 className="mb-1 text-2xl font-bold text-foreground">Dashboard</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        Visão geral em tempo real desta instância do Evolution GO.
      </p>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {isLoading || !stats ? (
          <>
            <StatCardSkeleton />
            <StatCardSkeleton />
            <StatCardSkeleton />
            <StatCardSkeleton />
          </>
        ) : (
          <>
            <StatCard
              label="Instâncias conectadas"
              value={String(stats.instances.connected)}
              sub={`${stats.instances.total} no total`}
              icon={Smartphone}
            />
            <StatCard
              label="Mensagens enviadas"
              value={stats.messages.sent.toLocaleString('pt-BR')}
              sub="desde o último start"
              icon={Send}
            />
            <StatCard
              label="Mensagens recebidas"
              value={stats.messages.received.toLocaleString('pt-BR')}
              sub="desde o último start"
              icon={Inbox}
            />
            <StatCard
              label="Uptime"
              value={formatUptime(stats.uptimeSeconds)}
              sub="processo atual"
              icon={Clock}
            />
          </>
        )}
      </div>

      {stats && stats.instances.total > 0 && (
        <Card className="mt-4 bg-sidebar border-sidebar-border">
          <CardContent className="p-5">
            <div className="mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              Instâncias por status
            </div>
            <div className="space-y-3">
              <div className="flex items-center gap-3 text-sm">
                <span className="w-24 shrink-0 text-muted-foreground">Conectadas</span>
                <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-green-500"
                    style={{
                      width: `${(stats.instances.connected / stats.instances.total) * 100}%`,
                    }}
                  />
                </div>
                <span className="w-8 shrink-0 text-right font-semibold tabular-nums">
                  {stats.instances.connected}
                </span>
              </div>
              <div className="flex items-center gap-3 text-sm">
                <span className="w-24 shrink-0 text-muted-foreground">Offline</span>
                <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-muted-foreground/40"
                    style={{
                      width: `${(stats.instances.disconnected / stats.instances.total) * 100}%`,
                    }}
                  />
                </div>
                <span className="w-8 shrink-0 text-right font-semibold tabular-nums">
                  {stats.instances.disconnected}
                </span>
              </div>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
