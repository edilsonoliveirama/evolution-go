/**
 * Typebot Settings Card
 * Configures the Typebot bot that answers the contacts of an instance
 */

import { useCallback, useEffect, useState } from 'react';
import { Button } from '@evoapi/design-system';
import { RefreshCw, Save, Trash2, XCircle } from 'lucide-react';
import { toast } from 'sonner';
import * as typebotApi from '@/services/api/typebot';
import type { TypebotConfig, TypebotConfigInput, TypebotSession } from '@/services/api/typebot';

interface TypebotSettingsCardProps {
  instanceToken: string;
}

const inputClass =
  'w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50';

const errorMessage = (error: unknown, fallback: string) =>
  (error as { message?: string })?.message || fallback;

function toInput(config: TypebotConfig): TypebotConfigInput {
  return {
    enabled: config.enabled,
    url: config.url || '',
    publicId: config.publicId || '',
    triggerType: config.triggerType || 'keyword',
    triggerKeywords: config.triggerKeywords || '',
    triggerOperator: config.triggerOperator || 'equals',
    expireMinutes: config.expireMinutes ?? 60,
    keywordFinish: config.keywordFinish ?? '#sair',
    delayMessageMs: config.delayMessageMs ?? 1000,
    unknownMessage: config.unknownMessage || '',
    stopBotFromMe: config.stopBotFromMe,
    handoffChatwoot: config.handoffChatwoot,
    ignoreJids: config.ignoreJids || '',
  };
}

interface ToggleRowProps {
  id: string;
  label: string;
  description: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
}

function ToggleRow({ id, label, description, checked, disabled, onChange }: ToggleRowProps) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div>
        <label htmlFor={id} className="text-sm font-medium text-foreground cursor-pointer">
          {label}
        </label>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      <input
        id={id}
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        className="rounded border-input w-4 h-4 shrink-0"
      />
    </div>
  );
}

const formatPhone = (jid: string) => jid.split('@')[0];

export default function TypebotSettingsCard({ instanceToken }: TypebotSettingsCardProps) {
  const [config, setConfig] = useState<TypebotConfig | null>(null);
  const [form, setForm] = useState<TypebotConfigInput | null>(null);
  const [sessions, setSessions] = useState<TypebotSession[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);

  const loadSessions = useCallback(async () => {
    try {
      setSessions(await typebotApi.getTypebotSessions(instanceToken));
    } catch {
      setSessions([]);
    }
  }, [instanceToken]);

  useEffect(() => {
    let cancelled = false;
    typebotApi
      .getTypebotConfig(instanceToken)
      .then((data) => {
        if (cancelled) return;
        setConfig(data);
        setForm(toInput(data));
        if (data.configured) loadSessions();
      })
      .catch((error) => {
        if (!cancelled) toast.error(errorMessage(error, 'Erro ao carregar configuração do Typebot'));
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [instanceToken, loadSessions]);

  const update = <K extends keyof TypebotConfigInput>(key: K, value: TypebotConfigInput[K]) =>
    setForm((prev) => (prev ? { ...prev, [key]: value } : prev));

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!form) return;
    setIsSaving(true);
    try {
      const saved = await typebotApi.saveTypebotConfig(instanceToken, {
        ...form,
        expireMinutes: Number(form.expireMinutes) || 0,
        delayMessageMs: Number(form.delayMessageMs) || 0,
      });
      setConfig(saved);
      setForm(toInput(saved));
      toast.success(saved.enabled ? 'Typebot ativado nesta instância' : 'Typebot desativado');
      loadSessions();
    } catch (error) {
      toast.error(errorMessage(error, 'Erro ao salvar configuração do Typebot'));
    } finally {
      setIsSaving(false);
    }
  };

  const handleRemove = async () => {
    if (!window.confirm('Remover a integração com o Typebot? As sessões ativas serão encerradas.')) return;
    setIsSaving(true);
    try {
      await typebotApi.deleteTypebotConfig(instanceToken);
      const fresh = await typebotApi.getTypebotConfig(instanceToken);
      setConfig(fresh);
      setForm(toInput(fresh));
      setSessions([]);
      toast.success('Integração com o Typebot removida');
    } catch (error) {
      toast.error(errorMessage(error, 'Erro ao remover integração'));
    } finally {
      setIsSaving(false);
    }
  };

  const handleCloseSessions = async () => {
    if (!window.confirm('Encerrar todas as sessões do bot? Os contatos recomeçam no próximo gatilho.')) return;
    try {
      await typebotApi.closeTypebotSessions(instanceToken);
      setSessions([]);
      toast.success('Sessões encerradas');
    } catch (error) {
      toast.error(errorMessage(error, 'Erro ao encerrar sessões'));
    }
  };

  if (isLoading || !form) {
    return (
      <div className="rounded-lg border border-sidebar-border bg-card p-6">
        <h2 className="text-lg font-semibold text-foreground mb-2">Typebot</h2>
        <p className="text-sm text-muted-foreground">
          {isLoading ? 'Carregando...' : 'Não foi possível carregar a configuração.'}
        </p>
      </div>
    );
  }

  const isActive = config?.configured && config.enabled;
  const fieldsDisabled = !form.enabled;
  const paused = sessions.filter((s) => s.status === 'paused').length;

  return (
    <form onSubmit={handleSave}>
      <div className="rounded-lg border border-sidebar-border bg-card p-6">
        <div className="flex items-center justify-between mb-4">
          <div>
            <h2 className="text-lg font-semibold text-foreground">Typebot</h2>
            <p className="text-xs text-muted-foreground">Um fluxo do Typebot atende os contatos desta instância</p>
          </div>
          {isActive && (
            <span className="rounded-full bg-green-500/10 px-3 py-1 text-xs font-medium text-green-500">Ativo</span>
          )}
        </div>

        <div className="space-y-4">
          <ToggleRow
            id="tb-enabled"
            label="Habilitar Typebot"
            description="Responde automaticamente as conversas individuais (grupos são ignorados)"
            checked={form.enabled}
            onChange={(v) => update('enabled', v)}
          />

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label htmlFor="tb-url" className="block text-sm font-medium text-foreground mb-1">
                URL do viewer
              </label>
              <input
                id="tb-url"
                type="url"
                placeholder="https://typechat.suaempresa.com"
                value={form.url}
                disabled={fieldsDisabled}
                onChange={(e) => update('url', e.target.value)}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-muted-foreground">Endereço onde os bots publicados abrem (não o builder)</p>
            </div>
            <div>
              <label htmlFor="tb-bot" className="block text-sm font-medium text-foreground mb-1">
                Nome público do bot
              </label>
              <input
                id="tb-bot"
                type="text"
                placeholder="meu-bot-abc123"
                value={form.publicId}
                disabled={fieldsDisabled}
                onChange={(e) => update('publicId', e.target.value)}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-muted-foreground">Final do link de compartilhamento (ou cole o link inteiro)</p>
            </div>
          </div>

          <div className="border-t border-sidebar-border pt-4 space-y-4">
            <h3 className="text-sm font-semibold text-foreground">Quando iniciar</h3>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
              <div>
                <label htmlFor="tb-trigger" className="block text-sm font-medium text-foreground mb-1">
                  Gatilho
                </label>
                <select
                  id="tb-trigger"
                  value={form.triggerType}
                  disabled={fieldsDisabled}
                  onChange={(e) => update('triggerType', e.target.value as TypebotConfigInput['triggerType'])}
                  className={inputClass}
                >
                  <option value="keyword">Palavra-chave</option>
                  <option value="all">Qualquer mensagem</option>
                  <option value="none">Nunca (desligado)</option>
                </select>
              </div>
              {form.triggerType === 'keyword' && (
                <>
                  <div>
                    <label htmlFor="tb-operator" className="block text-sm font-medium text-foreground mb-1">
                      A mensagem
                    </label>
                    <select
                      id="tb-operator"
                      value={form.triggerOperator}
                      disabled={fieldsDisabled}
                      onChange={(e) => update('triggerOperator', e.target.value as TypebotConfigInput['triggerOperator'])}
                      className={inputClass}
                    >
                      <option value="equals">é igual a</option>
                      <option value="startsWith">começa com</option>
                      <option value="contains">contém</option>
                    </select>
                  </div>
                  <div>
                    <label htmlFor="tb-keywords" className="block text-sm font-medium text-foreground mb-1">
                      Palavras-chave
                    </label>
                    <input
                      id="tb-keywords"
                      type="text"
                      placeholder="oi, festa, reserva"
                      value={form.triggerKeywords}
                      disabled={fieldsDisabled}
                      onChange={(e) => update('triggerKeywords', e.target.value)}
                      className={inputClass}
                    />
                  </div>
                </>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              Vale para contatos sem sessão ativa. Maiúsculas e acentos são ignorados.
            </p>
          </div>

          <div className="border-t border-sidebar-border pt-4 space-y-4">
            <h3 className="text-sm font-semibold text-foreground">Sessão</h3>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
              <div>
                <label htmlFor="tb-expire" className="block text-sm font-medium text-foreground mb-1">
                  Expira após (min)
                </label>
                <input
                  id="tb-expire"
                  type="number"
                  min={0}
                  value={form.expireMinutes}
                  disabled={fieldsDisabled}
                  onChange={(e) => update('expireMinutes', Number(e.target.value))}
                  className={inputClass}
                />
                <p className="mt-1 text-xs text-muted-foreground">Sem resposta do contato. 0 = nunca</p>
              </div>
              <div>
                <label htmlFor="tb-finish" className="block text-sm font-medium text-foreground mb-1">
                  Palavra para encerrar
                </label>
                <input
                  id="tb-finish"
                  type="text"
                  value={form.keywordFinish}
                  disabled={fieldsDisabled}
                  onChange={(e) => update('keywordFinish', e.target.value)}
                  className={`${inputClass} font-mono`}
                />
              </div>
              <div>
                <label htmlFor="tb-delay" className="block text-sm font-medium text-foreground mb-1">
                  Digitando entre mensagens (ms)
                </label>
                <input
                  id="tb-delay"
                  type="number"
                  min={0}
                  max={10000}
                  step={100}
                  value={form.delayMessageMs}
                  disabled={fieldsDisabled}
                  onChange={(e) => update('delayMessageMs', Number(e.target.value))}
                  className={inputClass}
                />
              </div>
            </div>

            <div>
              <label htmlFor="tb-unknown" className="block text-sm font-medium text-foreground mb-1">
                Resposta para opção inválida
              </label>
              <input
                id="tb-unknown"
                type="text"
                value={form.unknownMessage}
                disabled={fieldsDisabled}
                onChange={(e) => update('unknownMessage', e.target.value)}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-muted-foreground">
                Enviada com a lista de opções quando o contato não responde com uma delas
              </p>
            </div>

            <ToggleRow
              id="tb-stop"
              label="Pausar o bot quando um atendente responder"
              description="Mensagem enviada pelo celular, Chatwoot ou API pausa o bot nesse contato"
              checked={form.stopBotFromMe}
              disabled={fieldsDisabled}
              onChange={(v) => update('stopBotFromMe', v)}
            />
            <ToggleRow
              id="tb-handoff"
              label="Transferir para o Chatwoot ao terminar"
              description="No fim do fluxo, a conversa vai para a fila dos atendentes. Resolver a conversa no Chatwoot reinicia o bot"
              checked={form.handoffChatwoot}
              disabled={fieldsDisabled}
              onChange={(v) => update('handoffChatwoot', v)}
            />

            <div>
              <label htmlFor="tb-ignore" className="block text-sm font-medium text-foreground mb-1">
                Números ignorados
              </label>
              <textarea
                id="tb-ignore"
                rows={2}
                placeholder="5511999999999, 5521988888888"
                value={form.ignoreJids}
                disabled={fieldsDisabled}
                onChange={(e) => update('ignoreJids', e.target.value)}
                className={inputClass}
              />
            </div>
          </div>

          {config?.configured && (
            <div className="border-t border-sidebar-border pt-4">
              <div className="flex items-center justify-between mb-2">
                <h3 className="text-sm font-semibold text-foreground">
                  Sessões ativas: {sessions.length}
                  {paused > 0 && <span className="font-normal text-muted-foreground"> ({paused} pausadas)</span>}
                </h3>
                <div className="flex gap-2">
                  <Button type="button" variant="outline" size="icon" onClick={loadSessions} title="Atualizar">
                    <RefreshCw className="h-4 w-4" />
                  </Button>
                  {sessions.length > 0 && (
                    <Button type="button" variant="outline" onClick={handleCloseSessions} className="gap-2">
                      <XCircle className="h-4 w-4" />
                      Encerrar todas
                    </Button>
                  )}
                </div>
              </div>
              {sessions.length > 0 && (
                <ul className="max-h-40 overflow-y-auto rounded-md border border-input divide-y divide-sidebar-border text-sm">
                  {sessions.map((s) => (
                    <li key={s.remoteJid} className="flex items-center justify-between px-3 py-2">
                      <span className="font-mono text-foreground">{formatPhone(s.remoteJid)}</span>
                      <span className="text-xs text-muted-foreground">
                        {s.status === 'paused' ? 'pausada' : 'no bot'} ·{' '}
                        {new Date(s.updatedAt).toLocaleString('pt-BR')}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}

          <div className="flex flex-wrap justify-end gap-2 pt-2">
            {config?.configured && (
              <Button type="button" variant="destructive" onClick={handleRemove} disabled={isSaving} className="gap-2">
                <Trash2 className="h-4 w-4" />
                Remover
              </Button>
            )}
            <Button type="submit" disabled={isSaving} className="gap-2">
              <Save className="h-4 w-4" />
              {isSaving ? 'Salvando...' : 'Salvar Typebot'}
            </Button>
          </div>
        </div>
      </div>
    </form>
  );
}
