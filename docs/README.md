# Documentação

Este diretório centraliza a documentação técnica e visual do projeto, incluindo infraestrutura, modelos C4, fluxos operacionais e conteúdos de estratégia de negócio.

## Estrutura principal

- `infrastructure/`
  - `generate_topology.py`: script Python que gera um diagrama de infraestrutura usando a biblioteca `diagrams`.
  - `requirements.txt`: dependências Python necessárias para gerar a topologia.
- `c4-model/`
  - `workspace.dsl`: descrição do sistema em formato Structurizr.
  - `theme.json`: configuração visual para os diagramas C4.
- `flows/`
  - Documentos de fluxos do sistema e roteamento de conversas.
- `business-strategy/`
  - Documentos de estratégia e design de valor para o projeto.
- `generated/`
  - Saídas visuais geradas (PNG/SVG) a partir dos scripts de documentação.

## `docs/infrastructure`

Esta pasta contém o motor headless de geração de arquitetura e suas dependências.

### Conteúdo principal

- `generate_topology.py`
  - Gera um diagrama de arquitetura a partir de código.
  - Define um fluxo de infraestrutura assíncrona com os seguintes componentes:
    - `Meta API (WhatsApp)` como entrada externa.
    - `Next.js (Painel Handoff)` como interface de dashboard.
    - `Kafka` como broker de eventos.
    - `Redis` para estado e pub/sub.
    - `ScyllaDB` para armazenamento de histórico.
    - `LLM API` como motor de linguagem externo.
    - `Go` para os workers e a recepção de webhooks.
  - O script produz um gráfico em modo headless (`show=False`) e o salva em `docs/generated/topology.png`.
  - Aplica um estilo visual escuro com nós e arestas personalizados.

- `requirements.txt`
  - Lista as dependências Python mínimas para gerar o diagrama:
    - `diagrams`
    - `graphviz`

### Como gerar a topologia

1. Crie um ambiente virtual Python opcional:

```bash
python3 -m venv .venv
source .venv/bin/activate
```

2. Instale as dependências:

```bash
uv install -r docs/infrastructure/requirements.txt
```

3. Execute o gerador:

```bash
python docs/infrastructure/generate_topology.py
```

4. Verifique o diagrama gerado em:

```bash
docs/generated/topology.png
```

### Requisitos adicionais

- `Graphviz` deve estar instalado no sistema, já que `diagrams` o utiliza para renderizar os gráficos.

```bash
sudo apt update && sudo apt install graphviz
```

### Explicação do diagrama

Este diagrama mostra a arquitetura assíncrona do sistema, organizada em quatro camadas:

1. **Borda & Fronteira**
   - `Meta API (WhatsApp)` recebe mensagens externas.
   - `Next.js (Painel Handoff)` é o dashboard de visualização e atendimento.

2. **Camada de Ingestão**
   - `Ingress Webhook` recebe e valida as requisições.
   - `Kafka` atua como broker de eventos para desacoplar entrada e processamento.

3. **Camada Core (Headless)**
   - `Agent Worker` orquestra o fluxo de atendimento.
   - `Redis Cluster` guarda estado rápido e pub/sub para atualização em tempo real.
   - `LLM API` representa a chamada ao modelo de linguagem externo.

4. **Persistência**
   - `ScyllaDB` armazena o histórico de conversas e eventos.

**Fluxo principal**

- O webhook chega pela API WhatsApp.
- O evento é validado e enviado para Kafka.
- O worker consome a fila, atualiza o estado em Redis e consulta o LLM.
- O resultado é gravado em ScyllaDB e devolvido ao usuário.
- O dashboard recebe atualizações via pub/sub para handoff ou monitoramento.

## Uso da documentação

Este README é o índice da documentação do projeto. Para cada área:

- `docs/infrastructure`: geração automática de topologias.
- `docs/c4-model`: modelos estruturais C4.
- `docs/flows`: fluxos de conversa e handoff.
- `docs/business-strategy`: decisões de produto e estratégia B2B.
- `docs/generated`: artefatos visuais resultantes.

> Dica: use `docs/infrastructure/generate_topology.py` sempre que alterar a infraestrutura para manter o diagrama de arquitetura atualizado.

