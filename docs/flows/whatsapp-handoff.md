# Transição Assíncrona: O Handoff Perfeito para Vendas

Este documento detalha o fluxo de transição invisível entre a inteligência artificial e o operador humano. Em operações corporativas B2B de alto valor, o atrito deve ser zero. A passagem de bastão ocorre de forma totalmente assíncrona: o sistema altera a máquina de estados no cache distribuído e atualiza a interface de operação em tempo real, sem que o cliente final perceba qualquer mudança sistêmica.

## Diagrama de Sequência e Roteamento

```mermaid
%%{init: {
  'theme': 'base',
  'themeVariables': {
    'background': '#050505',
    'primaryColor': '#0A0A0C',
    'primaryTextColor': '#FFFFFF',
    'primaryBorderColor': '#00E5FF',
    'lineColor': '#00E5FF',
    'secondaryColor': '#121215',
    'tertiaryColor': '#0A0A0C',
    'noteBkgColor': '#121215',
    'noteTextColor': '#FFFFFF',
    'noteBorderColor': '#00E5FF',
    'actorBkg': '#0A0A0C',
    'actorBorder': '#00E5FF',
    'actorTextColor': '#FFFFFF',
    'signalColor': '#00E5FF',
    'signalTextColor': '#FFFFFF'
  }
}}%%
sequenceDiagram
    autonumber
    actor C as Cliente Final (WhatsApp)
    participant W as Ingress Webhook (Go)
    participant K as Kafka (Event Stream)
    participant O as Agent Worker (Go)
    participant R as Redis (State & PubSub)
    participant S as ScyllaDB (History)
    participant UI as Next.js Web App
    actor H as Agente Humano

    Note over C,O: Fase 1: Qualificação e Gatilho de Handoff
    C->>W: Mensagem de intenção forte (ex: "Vamos avançar")
    W->>K: Enfileira Evento
    K->>O: Consome Evento
    O->>R: GET session_state
    Note over O: IA detecta intenção de compra B2B<br/>e decide sair do circuito.
    
    O->>R: UPDATE session_state = "HANDOFF"
    O->>S: INSERT log (Motivo: Qualificação concluída)
    O->>R: PUBLISH channel:tenant_id "Novo Handoff"
    R-->>UI: Evento WebSocket recebido
    UI-->>H: Notificação visual na tela de Inbox

    Note over C,H: Fase 2: Operação Direta pelo Humano (Sem IA)
    C->>W: "Podemos agendar a reunião?"
    W->>K: Enfileira Evento
    K->>O: Consome Evento
    O->>R: GET session_state
    R-->>O: Retorna "HANDOFF"
    Note over O: Execução Headless: Worker bypassa<br/>a chamada LLM e age apenas como roteador.
    
    O->>S: INSERT message_log
    O->>R: PUBLISH channel:tenant_id "Nova Mensagem"
    R-->>UI: Atualiza o chat em tempo real
    
    H->>UI: Digita a resposta comercial
    UI->>O: POST /api/send (Payload)
    O->>C: Envia via Meta WhatsApp API
    O->>S: INSERT message_log (Autor: Humano)
```
