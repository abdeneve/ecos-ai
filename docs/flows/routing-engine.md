# Motor de Roteamento e Orquestração Headless

Este fluxo detalha o ciclo de vida de uma mensagem recebida via WhatsApp. O foco arquitetural é o isolamento de falhas: o webhook responde à Meta em milissegundos, enquanto os *workers* em Go processam a máquina de estados e a inteligência artificial de forma assíncrona.

```mermaid
%%{init: { 'theme': 'base', 'themeVariables': { 'background': '#0A0A0C', 'primaryColor': '#1A1A1D', 'primaryTextColor': '#FFFFFF', 'primaryBorderColor': '#00E5FF', 'lineColor': '#00E5FF', 'secondaryColor': '#00B8CC', 'tertiaryColor': '#121215' } } }%%
sequenceDiagram
    autonumber
    actor C as Cliente (WhatsApp)
    participant W as Ingress Webhook (Go)
    participant K as Kafka (Event Stream)
    participant R as Redis (Cache/State)
    participant S as ScyllaDB (History)
    participant O as Agent Worker (Go)
    participant LLM as Vertex AI / OpenAI

    C->>W: POST /webhook (Mensagem)
    W->>R: GET / SETNX message_id (Idempotência)
    alt Duplicado
        W-->>W: Drop Event (TTL 24h)
    else Novo
        W->>K: Produce Event (Partição hash: tenant_id)
        W-->>C: 200 OK (Latência < 5ms)
    end

    K->>O: Consume Event
    O->>R: GET session_state
    O->>S: GET recent_context (Time-Bucketed)
    
    O->>LLM: Iniciar Agentic Loop (Contexto + Estado)
    Note over O,LLM: Aplicação de Circuit Breaker e Timeout
    LLM-->>O: Intenção, Resposta e Próxima Ação
    
    O->>R: UPDATE session_state
    O->>S: INSERT message_log (Assíncrono)
    O->>C: POST /messages (Resposta IA)
```
