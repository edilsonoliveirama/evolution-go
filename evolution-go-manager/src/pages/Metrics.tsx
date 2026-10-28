import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Card, CardContent, Skeleton } from '@evoapi/design-system';
import { AlertTriangle, Inbox, MessagesSquare, RefreshCw, Send, Smartphone, UserPlus } from 'lucide-react';
import { format, formatDistanceToNowStrict, parseISO } from 'date-fns';
import { ptBR } from 'date-fns/locale';
import { toast } from 'sonner';
import * as monitoringApi from '@/services/api/monitoring';
import type { MonitoringDashboard, MonitoringEvent } from '@/services/api/monitoring';
import { fetchInstances } from '@/services/api/instances';
import { BarList, Heatmap, MessagesChart } from '@/components/monitoring/charts';
import { dayLabel, dayTooltip, nf } from '@/components/monitoring/format';
import AlertSettingsCard from '@/components/monitoring/AlertSettingsCard';
import { cn } from '@/utils/cn';

const REFRESH_MS = 60000;

const PERIODS = [
  { days: 1, label: 'Hoje' },
  { days: 7, label: '7 dias' },
  { days: 30, label: '30 dias' },
  { days: 90, label: '90 dias' },
];

const TYPE_LABELS: Record<string, string> = {
  text: 'Texto',
  audio: 'Áudio',
  image: 'Imagem',
  video: 'Vídeo',
  document: 'Documento',
  sticker: 'Figurinha',
  location: 'Localização',
  contact: 'Contato',
  reaction: 'Reação',
  poll: 'Enquete',
  interactive: 'Interativa',
  other: 'Outros',
};

const COMPONENT_LABELS: Record<string, string> = {
  webhook: 'Webhook',
  rabbitmq: 'RabbitMQ',
  nats: 'NATS',
  chatwoot: 'Chatwoot',
  typebot: 'Typebot',
  send: 'Envio pelo WhatsApp',
};

const EVENT_LABELS: Record<string, { label: string; tone: 'ok' | 'warn' | 'bad' | 'info' }> = {
  connected: { label: 'Conectou', tone: 'ok' },
  paired: { label: 'Pareada (QR lido)', tone: 'ok' },
  disconnected: { label: 'Desconectou', tone: 'warn' },
  connect_failure: { label: 'Falha ao conectar', tone: 'warn' },
  stream_replaced: { label: 'Sessão aberta em outro lugar', tone: 'warn' },
  logged_out: { label: 'Logout', tone: 'bad' },
  temp_ban: { label: 'Bloqueio temporário', tone: 'bad' },
  failure: { label: 'Falha de entrega', tone: 'warn' },
  service_start: { label: 'Evolution GO reiniciado', tone: 'info' },
};

const TONE_CLASS = {
  ok: 'bg-green-500',
  warn: 'bg-yellow-500',
  bad: 'bg-red-500',
  info: 'bg-muted-foreground/50',
};

const errorMessage = (error: unknown, fallback: string) =>
  (error as { message?: string })?.message || fallback;

function Kpi({ label, value, sub, icon: Icon }: { label: string; value: string; sub?: string; icon: React.ElementType }) {
  return (
    <Card className="bg-sidebar border-sidebar-border">
      <CardContent className="p-5">
        <div className="mb-3 flex items-center justify-between">
          <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{label}</span>
          <Icon className="h-4 w-4 text-muted-foreground" />
        </div>
        <div className="text-3xl font-bold tabular-nums text-foreground">{value}</div>
        {sub && <div className="mt-1 text-xs text-muted-foreground">{sub}</div>}
      </CardContent>
    </Card>
  );
}

function Section({ title, sub, children, className }: { title: string; sub?: string; children: React.ReactNode; className?: string }) {
  return (
    <Card className={cn('bg-sidebar border-sidebar-border', className)}>
      <CardContent className="p-5">
        <div className="mb-4">
          <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{title}</div>
          {sub && <div className="mt-1 text-xs text-muted-foreground">{sub}</div>}
        </div>
        {children}
      </CardContent>
    </Card>
  );
}

const pct = (part: number, total: number) => (total > 0 ? `${Math.round((part / total) * 100)}%` : '—');

export default function Metrics() {
  const [days, setDays] = useState(7);
  const [instanceId, setInstanceId] = useState('');
  const [data, setData] = useState<MonitoringDashboard | null>(null);
  const [events, setEvents] = useState<MonitoringEvent[]>([]);
  const [instances, setInstances] = useState<{ id: string; name: string }[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    fetchInstances()
      .then((list) => setInstances(list.map((i) => ({ id: i.id, name: i.instanceName }))))
      .catch(() => setInstances([]));
  }, []);

  const load = useCallback(async () => {
    try {
      const [dashboard, recent] = await Promise.all([
        monitoringApi.getDashboard(days, instanceId),
        monitoringApi.getEvents(instanceId, 50),
      ]);
      setData(dashboard);
      setEvents(recent);
    } catch (error) {
      toast.error(errorMessage(error, 'Não foi possível carregar as métricas.'));
    } finally {
      setIsLoading(false);
    }
  }, [days, instanceId]);

  useEffect(() => {
    setIsLoading(true);
    load();
    const interval = setInterval(load, REFRESH_MS);
    return () => clearInterval(interval);
  }, [load]);

  const chart = useMemo(() => {
    if (!data) return { points: [], labelEvery: 1 };
    if (data.hourly && data.hourly.length > 0 && data.days === 1) {
      return {
        points: data.hourly.map((h) => ({
          label: `${Number(h.hour.slice(11, 13))}h`,
          tooltip: `${h.hour.slice(11, 16)}: ${nf(h.in)} recebidas, ${nf(h.out)} enviadas`,
          in: h.in,
          out: h.out,
        })),
        labelEvery: 3,
      };
    }
    return {
      points: data.daily.map((d) => ({
        label: dayLabel(d.day),
        tooltip: `${dayTooltip(d.day)}: ${nf(d.in)} recebidas, ${nf(d.out)} enviadas, ${nf(d.newContacts)} contatos novos`,
        in: d.in,
        out: d.out,
      })),
      labelEvery: Math.max(1, Math.ceil(data.daily.length / 15)),
    };
  }, [data]);

  const totals = data?.totals;

  return (
    <div className="p-6">
      <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="mb-1 text-2xl font-bold text-foreground">Métricas</h1>
          <p className="text-sm text-muted-foreground">
            Volume de mensagens, saúde das instâncias e alertas
            {data && ` · fuso ${data.timezone}`}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <select
            value={instanceId}
            onChange={(e) => setInstanceId(e.target.value)}
            className="rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground"
          >
            <option value="">Todas as instâncias</option>
            {instances.map((i) => (
              <option key={i.id} value={i.id}>
                {i.name}
              </option>
            ))}
          </select>
          <div className="flex overflow-hidden rounded-md border border-input">
            {PERIODS.map((p) => (
              <button
                key={p.days}
                type="button"
                onClick={() => setDays(p.days)}
                className={cn(
                  'px-3 py-2 text-sm transition-colors',
                  days === p.days ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:bg-accent'
                )}
              >
                {p.label}
              </button>
            ))}
          </div>
          <Button type="button" variant="outline" size="icon" onClick={load} title="Atualizar">
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-5">
        {isLoading || !totals ? (
          Array.from({ length: 5 }, (_, i) => (
            <Card key={i} className="bg-sidebar border-sidebar-border">
              <CardContent className="p-5">
                <Skeleton className="mb-4 h-4 w-24" />
                <Skeleton className="h-8 w-16" />
              </CardContent>
            </Card>
          ))
        ) : (
          <>
            <Kpi label="Recebidas" value={nf(totals.messagesIn)} icon={Inbox} />
            <Kpi
              label="Enviadas"
              value={nf(totals.messagesOut)}
              sub={`${nf(totals.delivered)} entregues · ${nf(totals.read)} lidas`}
              icon={Send}
            />
            <Kpi label="Contatos novos" value={nf(totals.newContacts)} sub="primeira conversa no período" icon={UserPlus} />
            <Kpi label="Conversas ativas" value={nf(totals.activeChats)} sub="contatos com mensagens" icon={MessagesSquare} />
            <Kpi
              label="Instâncias online"
              value={`${totals.connected}/${totals.instanceCount}`}
              sub={`${nf(totals.disconnects)} quedas · ${nf(totals.failures)} falhas`}
              icon={totals.connected < totals.instanceCount ? AlertTriangle : Smartphone}
            />
          </>
        )}
      </div>

      {data && (
        <>
          <Section
            className="mt-4"
            title={data.days === 1 ? 'Mensagens por hora' : 'Mensagens por dia'}
            sub="Conversas individuais e grupos; status e canais não entram"
          >
            <MessagesChart points={chart.points} labelEvery={chart.labelEvery} />
          </Section>

          <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
            <Section
              className="lg:col-span-2"
              title="Horários de pico"
              sub={`Mensagens recebidas por dia da semana e hora${data.days > data.retentionDays ? ` (últimos ${data.retentionDays} dias)` : ''}`}
            >
              <Heatmap cells={data.heatmap || []} />
            </Section>
            <Section title="Tipos de mensagem">
              <BarList
                items={(data.types || []).map((t) => ({
                  label: TYPE_LABELS[t.type] || t.type,
                  value: t.in + t.out,
                  sub: `${nf(t.in)} recebidas, ${nf(t.out)} enviadas`,
                }))}
              />
            </Section>
          </div>

          <Section className="mt-4" title="Saúde das instâncias">
            <div className="overflow-x-auto">
              <table className="w-full min-w-[720px] text-sm">
                <thead>
                  <tr className="border-b border-sidebar-border text-left text-xs text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">Instância</th>
                    <th className="py-2 pr-4 font-medium">Status</th>
                    <th className="py-2 pr-4 text-right font-medium">Disponibilidade</th>
                    <th className="py-2 pr-4 text-right font-medium">Quedas</th>
                    <th className="py-2 pr-4 text-right font-medium">Logouts</th>
                    <th className="py-2 pr-4 text-right font-medium">Bloqueios</th>
                    <th className="py-2 pr-4 text-right font-medium">Falhas</th>
                    <th className="py-2 pr-4 text-right font-medium">Recebidas</th>
                    <th className="py-2 text-right font-medium">Enviadas</th>
                  </tr>
                </thead>
                <tbody>
                  {(data.instances || []).map((h) => (
                    <tr key={h.id} className="border-b border-sidebar-border/50 last:border-0">
                      <td className="py-2 pr-4 font-medium text-foreground">{h.name}</td>
                      <td className="py-2 pr-4">
                        <span className="flex items-center gap-2">
                          <span className={cn('h-2 w-2 rounded-full', h.connected ? 'bg-green-500' : 'bg-red-500')} />
                          {h.connected
                            ? 'Online'
                            : h.downSince
                              ? `Offline há ${formatDistanceToNowStrict(parseISO(h.downSince), { locale: ptBR })}`
                              : 'Offline'}
                        </span>
                      </td>
                      <td className="py-2 pr-4 text-right tabular-nums">
                        {h.uptimePercent === null ? '—' : `${h.uptimePercent.toFixed(1)}%`}
                      </td>
                      <td className="py-2 pr-4 text-right tabular-nums">{nf(h.disconnects)}</td>
                      <td className={cn('py-2 pr-4 text-right tabular-nums', h.loggedOuts > 0 && 'text-red-500')}>
                        {nf(h.loggedOuts)}
                      </td>
                      <td className={cn('py-2 pr-4 text-right tabular-nums', h.bans > 0 && 'text-red-500')}>{nf(h.bans)}</td>
                      <td className={cn('py-2 pr-4 text-right tabular-nums', h.failures > 0 && 'text-yellow-500')}>
                        {nf(h.failures)}
                      </td>
                      <td className="py-2 pr-4 text-right tabular-nums">{nf(h.messagesIn)}</td>
                      <td className="py-2 text-right tabular-nums">{nf(h.messagesOut)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="mt-3 text-xs text-muted-foreground">
              Disponibilidade é o tempo conectada no período, contando a partir do primeiro evento registrado.
              Taxa de leitura das enviadas: {totals ? pct(totals.read, totals.messagesOut) : '—'}.
            </p>
          </Section>

          <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
            <Section title="Falhas de entrega" sub="Webhook, filas, integrações e envios que não chegaram">
              <BarList
                items={(data.failures || []).map((f) => ({ label: COMPONENT_LABELS[f.component] || f.component, value: f.count }))}
              />
            </Section>
            <Section className="lg:col-span-2" title="Últimos eventos">
              {events.length === 0 ? (
                <p className="text-sm text-muted-foreground">Nenhum evento registrado ainda.</p>
              ) : (
                <ul className="max-h-80 space-y-2 overflow-y-auto pr-2">
                  {events.map((e) => {
                    const meta = EVENT_LABELS[e.kind] || { label: e.kind, tone: 'info' as const };
                    const detail = e.kind === 'failure' ? COMPONENT_LABELS[e.detail] || e.detail : e.detail;
                    return (
                      <li key={e.id} className="flex items-start gap-3 text-sm">
                        <span className={cn('mt-1.5 h-2 w-2 shrink-0 rounded-full', TONE_CLASS[meta.tone])} />
                        <span className="w-24 shrink-0 tabular-nums text-muted-foreground">
                          {format(parseISO(e.createdAt), 'dd/MM HH:mm')}
                        </span>
                        <span className="text-foreground">
                          {e.instanceName && <span className="font-medium">{e.instanceName}: </span>}
                          {meta.label}
                          {detail && <span className="text-muted-foreground"> ({detail})</span>}
                        </span>
                      </li>
                    );
                  })}
                </ul>
              )}
            </Section>
          </div>
        </>
      )}

      <div className="mt-4">
        <AlertSettingsCard instances={instances} />
      </div>
    </div>
  );
}
