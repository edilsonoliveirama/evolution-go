# Monitoramento

O Evolution GO registra o volume de mensagens e a saúde da conexão de cada instância. Os dados aparecem na página **Métricas** do manager, podem ser lidos pela API e também saem no formato Prometheus. Alertas avisam por WhatsApp e/ou webhook quando algo precisa de atenção.

Tudo é administrativo: as rotas exigem a `GLOBAL_API_KEY`.

## O que é medido

| Grupo | Métricas |
|-------|----------|
| Volume | Mensagens recebidas e enviadas por dia (ou por hora, no período "Hoje"), por tipo (texto, áudio, imagem...), mapa de horários de pico |
| Contatos | Contatos novos (primeira conversa desde que o monitoramento foi ativado) e conversas ativas (contatos com mensagens no período) |
| Entrega | Confirmações de entrega e leitura das mensagens enviadas em conversas individuais |
| Saúde | Disponibilidade (% do tempo conectada), quedas, logouts, bloqueios temporários, status atual |
| Falhas | Webhook, RabbitMQ, NATS, Chatwoot, Typebot e envios pelo WhatsApp que falharam |

Status, canais e mensagens de controle (edição, apagar, votos de enquete) não entram na contagem. Mensagens de grupo entram no volume, mas não em contatos e conversas.

### Armazenamento

- `metric_events`: um registro por evento, guardado por `METRICS_RETENTION_DAYS` (padrão 90 dias). Alimenta o mapa de horários, as conversas ativas, a disponibilidade e a linha do tempo.
- `metric_daily`: totais por dia, guardados para sempre. Alimenta volume, tipos e falhas de qualquer período.
- `metric_contacts`: primeira conversa de cada contato.

Os dias são contados no fuso `METRICS_TIMEZONE` (padrão `America/Sao_Paulo`). Os eventos são gravados em lote a cada 5 segundos; um reinício perde no máximo esses segundos.

## Alertas

Configurados na página Métricas ou em `PUT /monitoring/alerts`.

| Alerta | Quando |
|--------|--------|
| Instância desconectada | Offline há mais que o tempo configurado (padrão 5 min). Quedas curtas se reconectam sozinhas e não avisam |
| Voltou a conectar | Depois de um alerta de desconexão |
| Logout | Na hora: é preciso ler o QR Code de novo |
| Bloqueio temporário | Na hora |
| Falhas de entrega | N falhas do mesmo destino em 15 minutos (padrão 10; 0 desliga), no máximo 1 aviso por hora |

Depois de um reinício do Evolution GO, as instâncias que estavam conectadas devem voltar sozinhas; as que não voltarem dentro do prazo geram o alerta de desconexão.

**Canais:**

- **WhatsApp**: uma instância envia o aviso para os números configurados. Se for ela que cair, o aviso dela só chega pelo webhook. Os avisos aparecem como mensagens enviadas dessa instância (e no Chatwoot, se ela estiver integrada), então prefira uma instância só para isso.
- **Webhook**: `POST` com `{"event": "MonitoringAlert", "data": {...}}`. Útil para encaminhar por e-mail ou Telegram pelo n8n.

```json
{
  "event": "MonitoringAlert",
  "data": {
    "type": "instance_down",
    "instanceId": "81ce335a-...",
    "instanceName": "loja",
    "reason": "disconnected",
    "since": "2026-10-27T22:09:23-03:00",
    "at": "2026-10-27T22:14:23-03:00",
    "message": "⚠️ *Evolution GO*: a instância *loja* está desconectada desde 27/10 22:09 (conexão encerrada pelo WhatsApp)."
  }
}
```

`type`: `instance_down`, `instance_recovered`, `instance_logged_out`, `instance_banned`, `delivery_failures` ou `test`.

## API

| Rota | Descrição |
|------|-----------|
| `GET /monitoring/dashboard?days=7&instanceId=` | Tudo o que a página mostra. `days` de 1 a 365, contando hoje |
| `GET /monitoring/events?instanceId=&limit=100` | Últimos eventos de conexão e falhas |
| `GET /monitoring/alerts` | Configuração dos alertas |
| `PUT /monitoring/alerts` | Salva a configuração |
| `POST /monitoring/alerts/test` | Envia um alerta de teste para os canais salvos |
| `GET /metrics` | Prometheus |

## Prometheus

`GET /metrics` aceita a chave no header `apikey` ou como Bearer token:

```yaml
scrape_configs:
  - job_name: evolution-go
    metrics_path: /metrics
    authorization:
      credentials: <GLOBAL_API_KEY>
    static_configs:
      - targets: ["evolution-go:8080"]
```

| Métrica | Tipo | Labels |
|---------|------|--------|
| `evogo_instance_connected` | gauge | `instance_id`, `instance_name` |
| `evogo_messages_total` | counter | + `direction` (`in`/`out`), `type` |
| `evogo_connection_events_total` | counter | + `event` |
| `evogo_delivery_failures_total` | counter | + `component` |
| `evogo_receipts_total` | counter | + `status` (`delivered`/`read`) |
| `evogo_monitoring_dropped_events_total` | counter | |
| `evogo_process_uptime_seconds` | gauge | |

Os contadores começam do zero a cada reinício do processo, como é padrão no Prometheus.
