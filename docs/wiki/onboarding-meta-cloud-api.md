# Guia de Onboarding — Meta WhatsApp Cloud API

## Pré-requisitos

- Conta no **Meta Business Manager**: https://business.facebook.com/
- **WhatsApp Business Account (WABA)** já criada e aprovada
- Número de telefone comercial registrado e verificado na WABA
- Evolution GO compilado ou rodando com suporte a Cloud API (engine `cloudapi`)

## Passo a passo

### 1. Criar ou localizar sua WABA

1. Acesse [Meta Business Manager](https://business.facebook.com/)
2. Navegue para **Business Tools** → **WhatsApp Business Platform**
3. Selecione sua WABA existente ou crie uma nova
4. Após criada, anote o **WABA ID** (ex: `123456789012345`)

### 2. Registrar e verificar um número de telefone

1. No WhatsApp Business Platform, vá para **Phone Numbers**
2. Adicione um número ou selecione um existente
3. Complete a verificação do número (código SMS ou chamada)
4. Anote o **Phone Number ID** (ex: `987654321098765`)

### 3. Gerar um Access Token permanente

1. No Meta Developer Dashboard, vá para **Dashboard** → sua app → **WhatsApp**
2. Use o **Token Generator** e selecione a WABA e permissões mínimas:
   - `whatsapp_business_messaging`
   - `whatsapp_business_management`
3. Clique em **Generate Token**
4. Copie o token gerado (ex: `EAAx...`)

> **Recomendado:** Para produção, crie um **Access Token Permanente** via [Meta Business Manager](https://business.facebook.com/) → Business Settings → System Users → Create System User → Assign WhatsApp permissions → Generate Token.

### 4. Obter App Secret e Verify Token

1. No Meta Developer Dashboard, selecione sua app
2. Vá para **App Settings** → **Basic**
3. Anote o **App Secret**

Para o Verify Token, escolha uma string arbitrária (ex: `meu-verify-token-seguro`). Esta mesma string será usada no passo 5 e no campo `metaVerifyToken` da instância.

### 5. Configurar Webhook no Meta Developer Dashboard

1. No Meta Developer Dashboard, vá para **WhatsApp** → **Configuration**
2. Em **Callback URL**: `https://seu-dominio.com/meta/webhook/{instanceId}`
3. Em **Verify Token**: a mesma string do passo 4
4. Selecione os eventos desejados:
   - `messages` — recebimento de mensagens
   - `message_deliveries` — confirmação de entrega
   - `message_reads` — confirmação de leitura
5. Clique em **Verify and Save**

O Evolution GO responde ao handshake automaticamente via `GET /meta/webhook/{instanceId}?hub.mode=subscribe&hub.verify_token=...&hub.challenge=...`.

### 6. Configurar instância Cloud API no Evolution GO

Ao criar a instância no Evolution GO, defina os campos:

| Campo | Valor |
|---|---|
| `engine` | `cloudapi` |
| `metaWabaId` | WABA ID obtido no passo 1 |
| `metaPhoneNumberId` | Phone Number ID do passo 2 |
| `metaToken` | Access Token do passo 3 |
| `metaAppSecret` | App Secret do passo 4 |
| `metaVerifyToken` | Verify Token do passo 4 |

### 7. Conectar a instância

Chame `POST /instance/connect` para validar as credenciais contra a Meta API.

### 8. Testar com template de amostra

Envie um template de amostra via `POST /send/template`:

```json
{
  "number": "5511999999999",
  "template": {
    "name": "hello_world",
    "language": "pt_BR",
    "components": [
      {
        "type": "body",
        "parameters": []
      }
    ]
  }
}
```

## Operações suportadas vs. não suportadas

### Suportadas (Cloud API)

- Envio de texto (`/send/text`)
- Envio de mídia (`/send/media-url`, `/send/media-file`)
- Envio de template (`/send/template`)
- Webhook da Meta (recebimento de mensagens, status)

### Não suportadas (Cloud API)

- QR Code (`/instance/qr`) — retorna erro 4xx
- Pairing (`/instance/pair`) — retorna erro 4xx
- Botões, Listas, Carrosséis, Enquetes — retornam erro 4xx
- Sticker, Localização, Contato — retornam erro 4xx
- Status/Story — não suportado

## Troubleshooting

### Token expirado

**Sintoma:** `400 - access_token inválido ou expirado`
**Solução:** Gere um novo Access Token (passo 3) e atualize a instância.

### Webhook não configurado

**Sintoma:** Meta retorna `403 - verification failed`
**Solução:** Verifique se a URL de callback está correta e pública (HTTPS). Verifique se o verify_token na instância corresponde ao configurado no Meta Developer Dashboard.

### Instância desconectada inesperadamente

**Verifique:**
1. O access token não expirou
2. O número ainda está na WABA
3. A chave de criptografia (`META_CREDENTIALS_ENCRYPTION_KEY`) não foi alterada
4. Os logs da instância para detalhes

### Chave de criptografia inválida

**Sintoma:** `encryption_key_error` no status
**Solução:** Verifique `META_CREDENTIALS_ENCRYPTION_KEY` no `.env` (32 bytes em base64).
Gere uma nova com `GenerateEncryptionKey()` se necessário.
