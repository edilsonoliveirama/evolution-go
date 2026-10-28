/**
 * Alert Settings Card
 * Where the monitoring alerts go (WhatsApp numbers and/or webhook) and when they fire.
 */

import { useEffect, useState } from 'react';
import { Button } from '@evoapi/design-system';
import { Save, Send } from 'lucide-react';
import { toast } from 'sonner';
import * as monitoringApi from '@/services/api/monitoring';
import type { AlertConfig } from '@/services/api/monitoring';

const inputClass =
  'w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50';

const errorMessage = (error: unknown, fallback: string) =>
  (error as { message?: string })?.message || fallback;

interface AlertSettingsCardProps {
  instances: { id: string; name: string }[];
}

export default function AlertSettingsCard({ instances }: AlertSettingsCardProps) {
  const [form, setForm] = useState<AlertConfig | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [isTesting, setIsTesting] = useState(false);

  useEffect(() => {
    monitoringApi
      .getAlertConfig()
      .then(setForm)
      .catch((error) => toast.error(errorMessage(error, 'Erro ao carregar os alertas')));
  }, []);

  if (!form) {
    return (
      <div className="rounded-lg border border-sidebar-border bg-card p-6">
        <h2 className="text-lg font-semibold text-foreground">Alertas</h2>
        <p className="text-sm text-muted-foreground">Carregando...</p>
      </div>
    );
  }

  const update = <K extends keyof AlertConfig>(key: K, value: AlertConfig[K]) =>
    setForm((f) => (f ? { ...f, [key]: value } : f));

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSaving(true);
    try {
      setForm(await monitoringApi.saveAlertConfig(form));
      toast.success('Alertas salvos');
    } catch (error) {
      toast.error(errorMessage(error, 'Erro ao salvar os alertas'));
    } finally {
      setIsSaving(false);
    }
  };

  const handleTest = async () => {
    setIsTesting(true);
    try {
      await monitoringApi.sendTestAlert();
      toast.success('Alerta de teste enviado');
    } catch (error) {
      toast.error(errorMessage(error, 'Erro ao enviar o alerta de teste'));
    } finally {
      setIsTesting(false);
    }
  };

  const disabled = !form.enabled;

  return (
    <form onSubmit={handleSave}>
      <div className="rounded-lg border border-sidebar-border bg-card p-6">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold text-foreground">Alertas</h2>
            <p className="text-xs text-muted-foreground">
              Aviso quando uma instância cair, for deslogada, bloqueada ou tiver muitas falhas de entrega
            </p>
          </div>
          {form.enabled && (
            <span className="rounded-full bg-green-500/10 px-3 py-1 text-xs font-medium text-green-500">Ativo</span>
          )}
        </div>

        <div className="space-y-4">
          <div className="flex items-center justify-between gap-4">
            <div>
              <label htmlFor="al-enabled" className="cursor-pointer text-sm font-medium text-foreground">
                Habilitar alertas
              </label>
              <p className="text-xs text-muted-foreground">
                Logout e bloqueio avisam na hora; queda avisa depois do tempo abaixo, e de novo quando voltar
              </p>
            </div>
            <input
              id="al-enabled"
              type="checkbox"
              checked={form.enabled}
              onChange={(e) => update('enabled', e.target.checked)}
              className="h-4 w-4 shrink-0 rounded border-input"
            />
          </div>

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label htmlFor="al-notifier" className="mb-1 block text-sm font-medium text-foreground">
                Instância que envia os alertas
              </label>
              <select
                id="al-notifier"
                value={form.notifierInstanceId}
                disabled={disabled}
                onChange={(e) => update('notifierInstanceId', e.target.value)}
                className={inputClass}
              >
                <option value="">Nenhuma (só webhook)</option>
                {instances.map((i) => (
                  <option key={i.id} value={i.id}>
                    {i.name}
                  </option>
                ))}
              </select>
              <p className="mt-1 text-xs text-muted-foreground">
                Se for ela que cair, o aviso dela só chega pelo webhook
              </p>
            </div>
            <div>
              <label htmlFor="al-numbers" className="mb-1 block text-sm font-medium text-foreground">
                Números de WhatsApp
              </label>
              <input
                id="al-numbers"
                type="text"
                placeholder="5598999999999, 5511988888888"
                value={form.numbers}
                disabled={disabled}
                onChange={(e) => update('numbers', e.target.value)}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-muted-foreground">Com DDI e DDD, separados por vírgula</p>
            </div>
          </div>

          <div>
            <label htmlFor="al-webhook" className="mb-1 block text-sm font-medium text-foreground">
              Webhook (opcional)
            </label>
            <input
              id="al-webhook"
              type="url"
              placeholder="https://n8n.suaempresa.com/webhook/alertas"
              value={form.webhookUrl}
              disabled={disabled}
              onChange={(e) => update('webhookUrl', e.target.value)}
              className={inputClass}
            />
            <p className="mt-1 text-xs text-muted-foreground">
              Recebe um POST com {'{ event: "MonitoringAlert", data }'}. Útil para mandar e-mail ou Telegram pelo n8n
            </p>
          </div>

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label htmlFor="al-minutes" className="mb-1 block text-sm font-medium text-foreground">
                Avisar após desconectada por (min)
              </label>
              <input
                id="al-minutes"
                type="number"
                min={1}
                max={1440}
                value={form.disconnectMinutes}
                disabled={disabled}
                onChange={(e) => update('disconnectMinutes', Number(e.target.value))}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-muted-foreground">Quedas curtas se reconectam sozinhas e não avisam</p>
            </div>
            <div>
              <label htmlFor="al-failures" className="mb-1 block text-sm font-medium text-foreground">
                Falhas de entrega em 15 min para avisar
              </label>
              <input
                id="al-failures"
                type="number"
                min={0}
                max={10000}
                value={form.failureThreshold}
                disabled={disabled}
                onChange={(e) => update('failureThreshold', Number(e.target.value))}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-muted-foreground">
                Por instância e destino (webhook, Chatwoot...). 0 desliga. No máximo 1 aviso por hora
              </p>
            </div>
          </div>

          <div className="flex justify-end gap-2 border-t border-sidebar-border pt-4">
            <Button type="button" variant="outline" onClick={handleTest} disabled={isTesting} className="gap-2" title="Usa a configuração salva">
              <Send className="h-4 w-4" />
              {isTesting ? 'Enviando...' : 'Enviar teste'}
            </Button>
            <Button type="submit" disabled={isSaving} className="gap-2">
              <Save className="h-4 w-4" />
              {isSaving ? 'Salvando...' : 'Salvar'}
            </Button>
          </div>
        </div>
      </div>
    </form>
  );
}
