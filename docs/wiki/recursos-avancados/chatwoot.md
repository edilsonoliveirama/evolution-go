# Integração com Chatwoot

Cada instância pode ser ligada a uma caixa de entrada do tipo **API** no Chatwoot. As conversas individuais do WhatsApp aparecem no Chatwoot e as respostas dos atendentes são enviadas pelo WhatsApp.

Grupos, status e canais (newsletters) **não** são espelhados.

## Configurar pelo Manager

1. Abra a instância → **Configurações** → card **Chatwoot**.
2. Preencha:
   - **URL do Chatwoot**: ex. `https://chatwoot.suaempresa.com`
   - **ID da conta**: o número depois de `/accounts/` na URL do Chatwoot
   - **Token de acesso**: em *Perfil → Token de acesso*, de um usuário **administrador** da conta
   - **Nome da caixa de entrada**: por padrão, o nome da instância
   - **URL pública do Evolution GO**: o endereço pelo qual o Chatwoot alcança este servidor (já vem preenchido com o endereço do Manager)
3. Marque **Habilitar integração** e clique em **Salvar Chatwoot**.

Ao salvar, o Evolution GO valida o token, procura uma caixa de entrada do tipo API com esse nome (ou cria uma, se **Criar caixa de entrada automaticamente** estiver marcado) e configura o webhook dela para:

```
<URL pública>/chatwoot/webhook/<instanceId>/<segredo>
```

> ⚠️ **A URL pública precisa resolver para um IP público.** O Chatwoot 4.x recusa webhooks para IPs privados (proteção contra SSRF). Nomes internos de rede Docker (`http://evolution-go:4000`) ou `localhost` não funcionam, mesmo com os dois serviços no mesmo servidor: use o domínio público do Evolution GO.

## O que é sincronizado

| WhatsApp → Chatwoot | Chatwoot → WhatsApp |
|---|---|
| Texto, imagem, vídeo, áudio, documento, figurinha | Texto (Markdown convertido para a formatação do WhatsApp) |
| Localização, contatos, enquetes (como texto) | Anexos (imagem, vídeo, áudio, documento) |
| Respostas (citações) e reações | Respostas (citações) |
| Mensagens apagadas para todos (apaga no Chatwoot) | Mensagem apagada no Chatwoot é apagada para todos no WhatsApp |
| Mensagens editadas (nova mensagem "✏️ Mensagem editada") | Assinatura com o nome do atendente (opcional) |
| Mensagens enviadas pelo celular e pela API (como saída) | |
| Confirmação de leitura do contato | |

Se o envio para o WhatsApp falhar (instância desconectada, número inválido etc.), o Evolution GO deixa uma **nota privada** na conversa explicando o erro.

## Opções

| Opção | Efeito |
|---|---|
| Assinar mensagens | Prefixa `*Nome do atendente:*` + separador (`\n` = quebra de linha) |
| Reabrir conversa resolvida | Nova mensagem reabre a última conversa; desmarcado, cria uma nova |
| Iniciar conversas como pendentes | Conversas novas entram como *pendentes* (útil com bot de triagem) |
| Unificar contatos brasileiros | Números com e sem o 9º dígito são o mesmo contato |
| Números ignorados | Lista de números (separados por vírgula) que não vão para o Chatwoot |
| Importar contatos ao conectar | Cria no Chatwoot os contatos da agenda após o pareamento. Também há o botão **Importar contatos agora** |
| Importar histórico de mensagens | Importa as conversas dos últimos *N* dias recebidas no pareamento |

### Importação de histórico

O WhatsApp só envia o histórico **no momento do pareamento**. Habilite a opção e depois conecte (ou reconecte com novo QR Code) a instância.

Em **Dias de histórico**, `0` importa tudo o que o WhatsApp enviar. Por padrão o celular manda cerca de 1 ano; para pedir mais, defina no servidor `HISTORY_SYNC_DAYS_LIMIT` (ex.: `3650` para 10 anos) antes de parear. O celular precisa ficar online durante a sincronização, que pode levar de minutos a horas, e conversas de grupo não são importadas.

A importação grava direto no banco do Chatwoot, preservando a data original das mensagens e sem disparar notificações. Para habilitar, defina no servidor do Evolution GO:

```env
CHATWOOT_IMPORT_DATABASE_URI=postgres://usuario:senha@host:5432/chatwoot_production?sslmode=disable
```

Mídias do histórico são importadas como texto (ex.: "🖼️ Imagem").

## API

Todas as rotas usam o token da instância no header `apikey`.

| Método | Rota | Descrição |
|---|---|---|
| `GET` | `/chatwoot/config` | Configuração atual (ou valores padrão) |
| `PUT` | `/chatwoot/config` | Salva a configuração e sincroniza a caixa de entrada |
| `DELETE` | `/chatwoot/config` | Remove a integração (a caixa de entrada no Chatwoot é mantida) |
| `POST` | `/chatwoot/import-contacts` | Importa os contatos da agenda (em segundo plano) |
| `POST` | `/chatwoot/webhook/:instanceId/:secret` | Webhook chamado pelo Chatwoot (público, protegido pelo segredo) |

## Testes de integração

`pkg/chatwoot/service/integration_test.go` roda contra um Chatwoot real quando estas variáveis estão definidas: `CHATWOOT_TEST_URL`, `CHATWOOT_TEST_ACCOUNT`, `CHATWOOT_TEST_TOKEN`, `CHATWOOT_TEST_INBOX` e, opcionalmente, `CHATWOOT_TEST_DB` (importação). Sem elas, os testes são ignorados.
