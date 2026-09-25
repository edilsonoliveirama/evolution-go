# Capturar e Transcrever Conversas (Texto e Áudio)

Guia prático para um agente que consome a API do Evolution GO para capturar
mensagens de texto e áudio em tempo real e transcrever conversas completas.

Este documento complementa (não substitui) [Sistema de Eventos](../recursos-avancados/events-system.md)
e [Mensagens](./api-messages.md) — aqui o foco é só o caminho necessário para
captura + transcrição.

**Nível de confirmação de cada parte**: o formato de mensagens de **texto**
(seções 2 e 3) foi confirmado contra eventos reais capturados em 2026-09-25.
O formato de **áudio** (seção 4) foi derivado diretamente do código-fonte
(`pkg/whatsmeow/service/whatsmeow.go` e do `.pb.go` gerado do protocolo
whatsmeow) mas **não foi validado contra um evento de áudio real** — a lógica
é a mesma usada para texto (mesmo bloco de código monta o payload), então a
confiança é alta, mas vale seu agente logar o payload bruto da primeira
mensagem de áudio recebida em produção para confirmar antes de depender disso
cegamente.

> ⚠️ **O que NÃO funciona hoje**: `POST /chat/history-sync` (ver seção
> [Histórico retroativo](#histórico-retroativo-limitação-atual)) **não** entrega
> mensagens antigas via API/webhook, apesar do nome sugerir isso. O único caminho
> confiável é capturar mensagens **a partir do momento em que o webhook é
> configurado**, em tempo real.

---

## Índice

- [Visão geral do fluxo](#visão-geral-do-fluxo)
- [1. Configurar o webhook na instância](#1-configurar-o-webhook-na-instância)
- [2. Formato do payload recebido](#2-formato-do-payload-recebido)
- [3. Extrair texto](#3-extrair-texto)
- [4. Extrair e baixar áudio](#4-extrair-e-baixar-áudio)
- [5. Identificar a conversa (montar a transcrição)](#5-identificar-a-conversa-montar-a-transcrição)
- [Histórico retroativo (limitação atual)](#histórico-retroativo-limitação-atual)
- [Checklist de configuração recomendada](#checklist-de-configuração-recomendada)

---

## Visão geral do fluxo

```
WhatsApp (celular)
      │ mensagem nova (texto ou áudio)
      ▼
Evolution GO (whatsmeow) ──► evento events.Message
      │
      │ se WEBHOOK_FILES=true e é mídia: baixa o binário automaticamente
      │ (áudio já vem em base64 ou mediaUrl, sem 2ª chamada)
      ▼
POST <sua URL de webhook>   (retry: 5x, 30s de intervalo se falhar)
      │
      ▼
Seu agente:
  - texto  → data.Message.conversation ou .extendedTextMessage.text
  - áudio  → data.Message.{base64|mediaUrl} (metadados em .audioMessage) → serviço de transcrição
```

Não existe endpoint de "buscar histórico de mensagens já trocadas" pronto para
uso — a app grava metadados básicos de mensagens em `messages` no Postgres
(id, timestamp, status) apenas se `DATABASE_SAVE_MESSAGES=true`, **sem o
conteúdo**. Para transcrever conversas completas, o agente precisa estar
"ouvindo" o webhook continuamente e persistir ele mesmo o que for chegando.

---

## 1. Configurar o webhook na instância

Duas formas, mutuamente compatíveis:

**Global** (todas as instâncias, variável de ambiente):
```env
WEBHOOK_URL=https://seu-agente.exemplo.com/webhook
```

**Por instância** (via `POST /instance/connect`, autenticado com o **token da
instância**, não a API key global):
```bash
curl -X POST http://localhost:4000/instance/connect \
  -H "Content-Type: application/json" \
  -H "apikey: token-da-instancia" \
  -d '{
    "webhookUrl": "https://seu-agente.exemplo.com/webhook",
    "subscribe": ["MESSAGE"]
  }'
```

- `subscribe` é a lista de eventos que essa instância vai repassar para o
  webhook. Para este caso de uso, `MESSAGE` já é suficiente.
- Para desativar: reenviar com `"webhookUrl": "", "subscribe": []`.
- Sem HTTPS válido, prefira testar primeiro num ambiente que você controla
  (ex: um endpoint interno) — **o webhook entrega tudo que a instância recebe,
  incluindo conversas reais de quem estiver falando com o número conectado**.
  Não aponte o webhook para um serviço de terceiros/túnel público sem ter
  certeza do que vai trafegar por ele.
- Retry: se o seu endpoint não responder `2xx`, o Evolution GO tenta de novo
  até 5 vezes, com 30s de intervalo entre tentativas (`pkg/events/webhook`).

---

## 2. Formato do payload recebido

Todo evento chega como `POST` com este envelope (confirmado em produção,
`pkg/whatsmeow/service/whatsmeow.go`):

```json
{
  "event": "Message",
  "instanceId": "<uuid da instância>",
  "instanceName": "<nome da instância>",
  "instanceToken": "<token da instância>",
  "data": {
    "Info": {
      "Chat": "<jid do chat — individual ou @g.us para grupo>",
      "Sender": "<jid de quem enviou>",
      "SenderAlt": "<jid alternativo, formato @lid>",
      "IsFromMe": false,
      "IsGroup": false,
      "PushName": "<nome exibido do remetente>",
      "Timestamp": "2026-09-25T09:44:06-03:00",
      "Type": "text",
      "MediaType": ""
    },
    "Message": { "...": "conteúdo real, ver seções 3 e 4" }
  }
}
```

Note: `event` é a string `"Message"` (maiúscula na primeira letra) — **não**
`"MESSAGE"` como algum material antigo da wiki podia sugerir. Trate a
comparação sem case-sensitivity no seu agente por segurança.

`Info.Chat` é o identificador da conversa — use-o como chave para agrupar
mensagens de uma mesma transcrição. Termina em `@g.us` para grupos, em
`@s.whatsapp.net` para conversas individuais.

---

## 3. Extrair texto

Duas variações, dependendo de como a mensagem foi composta no WhatsApp:

**Mensagem de texto simples**:
```json
"Message": {
  "conversation": "Bom dia, tudo certo?"
}
```
→ texto está em `data.Message.conversation`.

**Mensagem de texto com preview de link, resposta a outra mensagem, ou
formatação estendida** (o caso mais comum na prática):
```json
"Message": {
  "extendedTextMessage": {
    "text": "Pode mandar novamente?",
    "contextInfo": { "...": "presente se for resposta a outra mensagem" }
  }
}
```
→ texto está em `data.Message.extendedTextMessage.text`.

**Regra prática**: tente `conversation` primeiro; se ausente, tente
`extendedTextMessage.text`. Se nenhum dos dois existir, a mensagem não é de
texto simples (pode ser áudio, imagem, figurinha, mensagem de sistema, etc).

Se for resposta a outra mensagem, o texto citado vem em
`data.Message.extendedTextMessage.contextInfo.quotedMessage.conversation` —
útil para reconstruir o encadeamento da conversa.

---

## 4. Extrair e baixar áudio

Mensagens de voz do WhatsApp chegam como `audioMessage`:

```json
"Message": {
  "audioMessage": {
    "mimetype": "audio/ogg; codecs=opus",
    "seconds": 7,
    "PTT": true,
    "fileLength": "12345",
    "...": "demais campos de criptografia (mediaKey, fileSHA256 etc) — não precisa deles se usar o base64/mediaUrl já resolvido abaixo"
  }
}
```

Campos relevantes para o agente:
- **`PTT: true`** → é uma mensagem de voz gravada (push-to-talk). `PTT: false`
  ou ausente → é um arquivo de áudio enviado como anexo (ex: uma música
  compartilhada). Filtre por `PTT: true` se só quer transcrever voz.
- **`seconds`** → duração em segundos.
- **`mimetype`** → tipicamente `audio/ogg; codecs=opus` (formato Opus/OGG do
  WhatsApp — a maioria dos serviços de transcrição, incluindo Whisper, aceita
  esse formato diretamente).

### Onde está o binário do áudio

Se a instância tiver `WEBHOOK_FILES=true` (padrão), o Evolution GO já baixa o
áudio automaticamente e anexa ao payload **no nível de `data.Message`** (não
dentro de `audioMessage`), em um dos dois formatos a seguir:

- **Se `MINIO_ENABLED=true`**: `data.Message.mediaUrl` (URL pronta para
  download) e `data.Message.mimetype`.
- **Se MinIO desabilitado**: `data.Message.base64` — o arquivo já em base64,
  pronto para decodificar e mandar direto pro serviço de transcrição.

```json
"Message": {
  "audioMessage": { "PTT": true, "seconds": 7, "mimetype": "audio/ogg; codecs=opus" },
  "base64": "T2dnUwACAAAAAAAAAAB..."
}
```

Isso significa que, na configuração padrão, **não é preciso chamar nenhum
endpoint adicional** para obter o áudio — ele já vem no próprio webhook.

### Caminho alternativo: `POST /message/downloadmedia`

Só é necessário se `WEBHOOK_FILES=false`, ou se você quiser rebaixar a mídia
depois (ex: reprocessar um áudio antigo). Requer reenviar o objeto `Message`
completo tal como veio no webhook:

```bash
curl -X POST http://localhost:4000/message/downloadmedia \
  -H "Content-Type: application/json" \
  -H "apikey: token-da-instancia" \
  -d '{"message": <objeto Message completo do payload original>}'
```
Retorna o binário como data URL (`dataurl.DataURL`). Suporta imagem, áudio,
documento, vídeo e figurinha (`pkg/message/service/message_service.go`).

---

## 5. Identificar a conversa (montar a transcrição)

Para reconstruir uma conversa completa em ordem, agrupe eventos recebidos por:

| Campo | Uso |
|---|---|
| `data.Info.Chat` | chave da conversa (uma por número/grupo) |
| `data.Info.Timestamp` | ordenação cronológica (ISO 8601, já com timezone) |
| `data.Info.PushName` | nome de exibição de quem enviou (não confiável como identificador único — pode mudar) |
| `data.Info.Sender` | JID de quem enviou — mais estável que `PushName` para identificar o autor |
| `data.Info.IsFromMe` | `true` = enviado pela própria instância (não por quem você está transcrevendo) |
| `data.Info.IsGroup` | `true` se `Chat` termina em `@g.us` |

Um item de transcrição fica, por exemplo:
```
[2026-09-25T09:44:06-03:00] Osineide (grupo): "Gente, bom dia!! Nós temos colinha..."
[2026-09-25T09:44:10-03:00] A. M (grupo): "Vemmm!" (respondendo a: "Vou tentar passar hoje...")
```

---

## Histórico retroativo (limitação atual)

O endpoint `POST /chat/history-sync` existe (`pkg/chat/service/chat_service.go`)
e de fato dispara `whatsmeow.BuildHistorySyncRequest` para o dispositivo
companheiro — mas a resposta chega depois, de forma assíncrona, como o evento
`events.HistorySync`. **Hoje esse evento só é logado internamente
(`pkg/whatsmeow/service/whatsmeow.go:1830`); seu conteúdo não é extraído nem
repassado ao webhook.** Ou seja: chamar esse endpoint não devolve mensagens
antigas para o seu agente no estado atual do código.

Se precisar de histórico retroativo de verdade, as opções são:
1. Implementar o parsing do evento `HistorySync` (ele contém os dados brutos
   de sincronização do protocolo WhatsApp) e expô-lo via webhook — trabalho de
   desenvolvimento, não é algo que já funciona hoje.
2. Aceitar a limitação e só capturar dali para frente, a partir do momento em
   que o webhook for configurado.

---

## Checklist de configuração recomendada

- [ ] `WEBHOOK_FILES=true` (padrão) — evita uma segunda chamada para baixar áudio
- [ ] `DATABASE_SAVE_MESSAGES` — decidir se quer manter metadados no Postgres
      além do que o agente já vai persistir por conta própria
- [ ] `EVENT_IGNORE_STATUS=true` (padrão) — evita ruído de status/stories no
      webhook, que não fazem parte de conversas
- [ ] Decidir `EVENT_IGNORE_GROUP` conforme o agente deva ou não transcrever
      grupos
- [ ] Testar o webhook contra um endpoint que você controla antes de apontar
      para produção — o payload inclui conteúdo real de conversas assim que
      a instância estiver conectada e o webhook ativo
