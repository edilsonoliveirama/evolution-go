/**
 * Chatwoot Settings Card
 * Configures the Chatwoot integration (API inbox) of an instance
 */

import { useEffect, useState } from 'react';
import { Button } from '@evoapi/design-system';
import { Copy, Eye, EyeOff, Save, Trash2, Users } from 'lucide-react';
import { toast } from 'sonner';
import * as chatwootApi from '@/services/api/chatwoot';
import type { ChatwootConfig, ChatwootConfigInput } from '@/services/api/chatwoot';

interface ChatwootSettingsCardProps {
  instanceName: string;
  instanceToken: string;
}

const inputClass =
  'w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50';

const errorMessage = (error: unknown, fallback: string) =>
  (error as { message?: string })?.message || fallback;

function toInput(config: ChatwootConfig, instanceName: string): ChatwootConfigInput {
  return {
    enabled: config.enabled,
    url: config.url || '',
    accountId: config.accountId || 0,
    token: config.token || '',
    inboxName: config.inboxName || instanceName,
    autoCreateInbox: config.autoCreateInbox,
    webhookBaseUrl: config.webhookBaseUrl || window.location.origin,
    signMessages: config.signMessages,
    signDelimiter: config.signDelimiter || '\\n',
    reopenConversation: config.reopenConversation,
    conversationPending: config.conversationPending,
    mergeBrazilContacts: config.mergeBrazilContacts,
    ignoreJids: config.ignoreJids || '',
    importContacts: config.importContacts,
    importMessages: config.importMessages,
    daysLimitImport: config.daysLimitImport || 3,
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

export default function ChatwootSettingsCard({ instanceName, instanceToken }: ChatwootSettingsCardProps) {
  const [config, setConfig] = useState<ChatwootConfig | null>(null);
  const [form, setForm] = useState<ChatwootConfigInput | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [showToken, setShowToken] = useState(false);

  useEffect(() => {
    let cancelled = false;
    chatwootApi
      .getChatwootConfig(instanceToken)
      .then((data) => {
        if (cancelled) return;
        setConfig(data);
        setForm(toInput(data, instanceName));
      })
      .catch((error) => {
        if (!cancelled) toast.error(errorMessage(error, 'Erro ao carregar configuração do Chatwoot'));
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [instanceToken, instanceName]);

  const update = <K extends keyof ChatwootConfigInput>(key: K, value: ChatwootConfigInput[K]) =>
    setForm((prev) => (prev ? { ...prev, [key]: value } : prev));

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!form) return;
    setIsSaving(true);
    try {
      const saved = await chatwootApi.saveChatwootConfig(instanceToken, {
        ...form,
        accountId: Number(form.accountId) || 0,
        daysLimitImport: Number(form.daysLimitImport) || 3,
      });
      setConfig(saved);
      setForm(toInput(saved, instanceName));
      toast.success(
        saved.enabled
          ? `Chatwoot conectado à caixa de entrada "${saved.inboxName}"`
          : 'Integração com o Chatwoot desativada'
      );
    } catch (error) {
      toast.error(errorMessage(error, 'Erro ao salvar configuração do Chatwoot'));
    } finally {
      setIsSaving(false);
    }
  };

  const handleRemove = async () => {
    if (!window.confirm('Remover a integração com o Chatwoot? A caixa de entrada no Chatwoot não será apagada.')) {
      return;
    }
    setIsSaving(true);
    try {
      await chatwootApi.deleteChatwootConfig(instanceToken);
      const fresh = await chatwootApi.getChatwootConfig(instanceToken);
      setConfig(fresh);
      setForm(toInput(fresh, instanceName));
      toast.success('Integração com o Chatwoot removida');
    } catch (error) {
      toast.error(errorMessage(error, 'Erro ao remover integração'));
    } finally {
      setIsSaving(false);
    }
  };

  const handleImportContacts = async () => {
    try {
      await chatwootApi.importChatwootContacts(instanceToken);
      toast.success('Importação de contatos iniciada. Pode levar alguns minutos.');
    } catch (error) {
      toast.error(errorMessage(error, 'Erro ao iniciar importação de contatos'));
    }
  };

  const copyWebhook = async () => {
    if (!config?.webhookUrl) return;
    try {
      await navigator.clipboard.writeText(config.webhookUrl);
      toast.success('URL copiada');
    } catch {
      toast.error('Não foi possível copiar');
    }
  };

  if (isLoading || !form) {
    return (
      <div className="rounded-lg border border-sidebar-border bg-card p-6">
        <h2 className="text-lg font-semibold text-foreground mb-2">Chatwoot</h2>
        <p className="text-sm text-muted-foreground">
          {isLoading ? 'Carregando...' : 'Não foi possível carregar a configuração.'}
        </p>
      </div>
    );
  }

  const isActive = config?.configured && config.enabled && config.inboxId > 0;
  const fieldsDisabled = !form.enabled;

  return (
    <form onSubmit={handleSave}>
      <div className="rounded-lg border border-sidebar-border bg-card p-6">
        <div className="flex items-center justify-between mb-4">
          <div>
            <h2 className="text-lg font-semibold text-foreground">Chatwoot</h2>
            <p className="text-xs text-muted-foreground">
              Atenda as conversas desta instância pelo Chatwoot
            </p>
          </div>
          {isActive && (
            <span className="rounded-full bg-green-500/10 px-3 py-1 text-xs font-medium text-green-500">
              Ativo · caixa #{config.inboxId}
            </span>
          )}
        </div>

        <div className="space-y-4">
          <ToggleRow
            id="cw-enabled"
            label="Habilitar integração"
            description="Espelha as conversas do WhatsApp em uma caixa de entrada do tipo API no Chatwoot"
            checked={form.enabled}
            onChange={(v) => update('enabled', v)}
          />

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div className="md:col-span-2">
              <label htmlFor="cw-url" className="block text-sm font-medium text-foreground mb-1">
                URL do Chatwoot
              </label>
              <input
                id="cw-url"
                type="url"
                placeholder="https://chatwoot.suaempresa.com"
                value={form.url}
                disabled={fieldsDisabled}
                onChange={(e) => update('url', e.target.value)}
                className={inputClass}
              />
            </div>

            <div>
              <label htmlFor="cw-account" className="block text-sm font-medium text-foreground mb-1">
                ID da conta
              </label>
              <input
                id="cw-account"
                type="number"
                min={1}
                placeholder="1"
                value={form.accountId || ''}
                disabled={fieldsDisabled}
                onChange={(e) => update('accountId', Number(e.target.value))}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-muted-foreground">
                Número após <code>/accounts/</code> na URL do Chatwoot
              </p>
            </div>

            <div>
              <label htmlFor="cw-token" className="block text-sm font-medium text-foreground mb-1">
                Token de acesso
              </label>
              <div className="relative">
                <input
                  id="cw-token"
                  type={showToken ? 'text' : 'password'}
                  autoComplete="off"
                  value={form.token}
                  disabled={fieldsDisabled}
                  onChange={(e) => update('token', e.target.value)}
                  className={`${inputClass} pr-10`}
                />
                <button
                  type="button"
                  onClick={() => setShowToken(!showToken)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  title={showToken ? 'Ocultar token' : 'Mostrar token'}
                >
                  {showToken ? <EyeOff size={16} /> : <Eye size={16} />}
                </button>
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                Perfil → Token de acesso (de um administrador da conta)
              </p>
            </div>

            <div>
              <label htmlFor="cw-inbox" className="block text-sm font-medium text-foreground mb-1">
                Nome da caixa de entrada
              </label>
              <input
                id="cw-inbox"
                type="text"
                value={form.inboxName}
                disabled={fieldsDisabled}
                onChange={(e) => update('inboxName', e.target.value)}
                className={inputClass}
              />
            </div>

            <div>
              <label htmlFor="cw-public-url" className="block text-sm font-medium text-foreground mb-1">
                URL pública do Evolution GO
              </label>
              <input
                id="cw-public-url"
                type="url"
                value={form.webhookBaseUrl}
                disabled={fieldsDisabled}
                onChange={(e) => update('webhookBaseUrl', e.target.value)}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-muted-foreground">
                Domínio público pelo qual o Chatwoot envia as respostas. Endereços internos
                (localhost, rede Docker) são bloqueados pelo Chatwoot.
              </p>
            </div>
          </div>

          <ToggleRow
            id="cw-autocreate"
            label="Criar caixa de entrada automaticamente"
            description="Cria a caixa do tipo API no Chatwoot se não existir uma com esse nome"
            checked={form.autoCreateInbox}
            disabled={fieldsDisabled}
            onChange={(v) => update('autoCreateInbox', v)}
          />

          {config?.webhookUrl && (
            <div>
              <label className="block text-sm font-medium text-foreground mb-1">Webhook da caixa de entrada</label>
              <div className="flex items-center gap-2">
                <input readOnly value={config.webhookUrl} className={`${inputClass} font-mono text-xs`} />
                <Button type="button" variant="outline" size="icon" onClick={copyWebhook} title="Copiar">
                  <Copy className="h-4 w-4" />
                </Button>
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                Configurado automaticamente na caixa de entrada. Mantenha em segredo.
              </p>
            </div>
          )}

          <div className="border-t border-sidebar-border pt-4 space-y-4">
            <h3 className="text-sm font-semibold text-foreground">Comportamento</h3>

            <ToggleRow
              id="cw-sign"
              label="Assinar mensagens"
              description="Adiciona o nome do atendente antes da mensagem enviada ao cliente"
              checked={form.signMessages}
              disabled={fieldsDisabled}
              onChange={(v) => update('signMessages', v)}
            />
            {form.signMessages && (
              <div>
                <label htmlFor="cw-delimiter" className="block text-sm font-medium text-foreground mb-1">
                  Separador da assinatura
                </label>
                <input
                  id="cw-delimiter"
                  type="text"
                  value={form.signDelimiter}
                  disabled={fieldsDisabled}
                  onChange={(e) => update('signDelimiter', e.target.value)}
                  className={`${inputClass} max-w-xs font-mono`}
                />
                <p className="mt-1 text-xs text-muted-foreground">
                  Use <code>\n</code> para quebra de linha
                </p>
              </div>
            )}

            <ToggleRow
              id="cw-reopen"
              label="Reabrir conversa resolvida"
              description="Novas mensagens reabrem a última conversa em vez de criar outra"
              checked={form.reopenConversation}
              disabled={fieldsDisabled}
              onChange={(v) => update('reopenConversation', v)}
            />
            <ToggleRow
              id="cw-pending"
              label="Iniciar conversas como pendentes"
              description="Útil quando um bot faz a triagem antes do atendente"
              checked={form.conversationPending}
              disabled={fieldsDisabled}
              onChange={(v) => update('conversationPending', v)}
            />
            <ToggleRow
              id="cw-merge-br"
              label="Unificar contatos brasileiros (9º dígito)"
              description="Trata números com e sem o 9 como o mesmo contato"
              checked={form.mergeBrazilContacts}
              disabled={fieldsDisabled}
              onChange={(v) => update('mergeBrazilContacts', v)}
            />

            <div>
              <label htmlFor="cw-ignore" className="block text-sm font-medium text-foreground mb-1">
                Números ignorados
              </label>
              <textarea
                id="cw-ignore"
                rows={2}
                placeholder="5511999999999, 5521988888888"
                value={form.ignoreJids}
                disabled={fieldsDisabled}
                onChange={(e) => update('ignoreJids', e.target.value)}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-muted-foreground">
                Conversas com esses números não vão para o Chatwoot. Grupos são sempre ignorados.
              </p>
            </div>
          </div>

          <div className="border-t border-sidebar-border pt-4 space-y-4">
            <h3 className="text-sm font-semibold text-foreground">Importação</h3>

            <ToggleRow
              id="cw-import-contacts"
              label="Importar contatos ao conectar"
              description="Cria no Chatwoot os contatos da agenda do WhatsApp após o pareamento"
              checked={form.importContacts}
              disabled={fieldsDisabled}
              onChange={(v) => update('importContacts', v)}
            />
            <ToggleRow
              id="cw-import-messages"
              label="Importar histórico de mensagens"
              description={
                config?.importAvailable
                  ? 'Importa as conversas recentes recebidas no pareamento (conecte a instância depois de habilitar)'
                  : 'Indisponível: defina CHATWOOT_IMPORT_DATABASE_URI no servidor (acesso ao banco do Chatwoot)'
              }
              checked={form.importMessages}
              disabled={fieldsDisabled || !config?.importAvailable}
              onChange={(v) => update('importMessages', v)}
            />
            {form.importMessages && config?.importAvailable && (
              <div>
                <label htmlFor="cw-days" className="block text-sm font-medium text-foreground mb-1">
                  Dias de histórico
                </label>
                <input
                  id="cw-days"
                  type="number"
                  min={1}
                  max={365}
                  value={form.daysLimitImport}
                  disabled={fieldsDisabled}
                  onChange={(e) => update('daysLimitImport', Number(e.target.value))}
                  className={`${inputClass} max-w-[8rem]`}
                />
              </div>
            )}
          </div>

          <div className="flex flex-wrap justify-end gap-2 pt-2">
            {config?.configured && (
              <Button type="button" variant="destructive" onClick={handleRemove} disabled={isSaving} className="gap-2">
                <Trash2 className="h-4 w-4" />
                Remover
              </Button>
            )}
            {isActive && (
              <Button type="button" variant="outline" onClick={handleImportContacts} disabled={isSaving} className="gap-2">
                <Users className="h-4 w-4" />
                Importar contatos agora
              </Button>
            )}
            <Button type="submit" disabled={isSaving} className="gap-2">
              <Save className="h-4 w-4" />
              {isSaving ? 'Salvando...' : 'Salvar Chatwoot'}
            </Button>
          </div>
        </div>
      </div>
    </form>
  );
}
