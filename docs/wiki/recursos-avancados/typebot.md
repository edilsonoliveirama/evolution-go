# Integração com Typebot

Cada instância pode ter um fluxo do [Typebot](https://typebot.io) respondendo automaticamente às conversas individuais do WhatsApp. Grupos, status e canais são ignorados.

Funciona com Typebot 3.x (validado na 3.19.1, bots de schema 6.x) pela API pública de chat do **viewer**. Não precisa de token do Typebot.

## Configurar pelo Manager

1. Publique o bot no Typebot.
2. No Manager, abra a instância → **Configurações** → card **Typebot**.
3. Preencha:
   - **URL do viewer**: o endereço onde os bots publicados abrem (ex. `https://typechat.suaempresa.com`), **não** o do builder.
   - **Nome público do bot**: o final do link de compartilhamento (ex. `meu-bot-abc123`). Também é possível colar o link inteiro.
   - **Gatilho**: palavra-chave (é igual a / começa com / contém), qualquer mensagem, ou nunca.
4. Marque **Habilitar Typebot** e clique em **Salvar Typebot**. O Evolution GO confere se o bot está publicado antes de salvar.

## Como a conversa funciona

- Um contato sem sessão ativa que envia o gatilho inicia o fluxo.
- As bolhas do bot são enviadas em ordem, com "digitando..." entre elas. Texto (com formatação), imagem, vídeo `.mp4` e áudio vão como mídia; YouTube/Vimeo e embeds vão como link. As **esperas** do fluxo são respeitadas (até 60 s cada).
- **Escolhas** viram uma lista numerada. O contato pode responder com o número (`1`, `1.`, `1)`) ou com o texto da opção, sem diferenciar maiúsculas, acentos ou pontuação. Se a resposta não corresponder a nenhuma opção, o bot reenvia a lista com a mensagem de **opção inválida**.
- Variáveis pré-preenchidas no início (crie no fluxo com estes nomes para usá-las): `remoteJid`, `pushName`, `phone`, `instanceName`.
- Áudios, figurinhas e mídias sem legenda enviados pelo contato são ignorados pelo bot.

### Encerrar, pausar e expirar

| Situação | O que acontece |
|---|---|
| Fluxo chega ao fim | Sessão encerrada. Com **Transferir para o Chatwoot** marcado, a conversa vai para a fila dos atendentes |
| Contato envia a palavra para encerrar (padrão `#sair`) | Sessão encerrada |
| Atendente responde (celular, Chatwoot ou API) | Bot **pausado** nesse contato (opção *Pausar o bot quando um atendente responder*) |
| Conversa **resolvida** no Chatwoot | Sessão encerrada; o bot volta a atender no próximo gatilho |
| Contato fica sem responder pelo tempo de expiração | Sessão encerrada na próxima mensagem |

O card mostra as sessões ativas e pausadas e tem o botão **Encerrar todas**.

## Usando junto com o Chatwoot

Com as duas integrações ativas na mesma instância:

1. No card **Chatwoot**, marque **Iniciar conversas como pendentes** — a conversa fica em *Pendentes* enquanto o bot atende.
2. No card **Typebot**, marque **Transferir para o Chatwoot ao terminar**.

As mensagens do bot e do contato aparecem na conversa do Chatwoot. Quando o fluxo termina, a conversa passa para *Abertas* (fila dos atendentes). Se um atendente responder antes, o bot é pausado. Ao resolver a conversa, o bot volta a atender esse contato na próxima vez.

## API

Todas as rotas usam o token da instância no header `apikey`.

| Método | Rota | Descrição |
|---|---|---|
| `GET` | `/typebot/config` | Configuração atual (ou valores padrão) |
| `PUT` | `/typebot/config` | Salva a configuração (valida o bot no viewer) |
| `DELETE` | `/typebot/config` | Remove a integração e as sessões |
| `GET` | `/typebot/sessions` | Sessões ativas/pausadas |
| `DELETE` | `/typebot/sessions` | Encerra todas as sessões |

## Testes de integração

`pkg/typebot/service/integration_test.go` e `service_integration_test.go` rodam contra um viewer real quando `TYPEBOT_TEST_URL` e `TYPEBOT_TEST_BOT` estão definidas (cada execução cria um resultado no bot — use um bot de teste). Sem elas, os testes são ignorados.
