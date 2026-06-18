# ecos-ai

Projeto de documentação e infraestrutura para um ecossistema de IA conversacional.

## Visão geral

Este repositório centraliza a documentação, topologia e orquestração da infraestrutura local necessária para um projeto de chatbot e automação B2B.

## Estrutura do projeto

- `docker-compose.yml`
  - Infraestrutura local core:
    - `redpanda`: broker de eventos Kafka-like
    - `redis`: cache e estado rápido
    - `scylla`: banco de dados de persistência
- `Makefile`
  - Comandos para gerenciar a infraestrutura local:
    - `make infra-up`
    - `make infra-down`
    - `make infra-logs`
    - `make infra-clean`
- `docs/`
  - `c4-model/`
    - `workspace.dsl`: visão macro do sistema em Structurizr
    - `theme.json`: configuração visual de cores e formas
  - `infrastructure/`
    - `generate_topology.py`: script Python para gerar diagramas de arquitetura usando `diagrams`
    - `requirements.txt`: dependências Python necessárias
  - `flows/`
    - `routing-engine.md`: diagramas Mermaid sobre máquina de estados e roteamento
    - `whatsapp-handoff.md`: diagramas Mermaid do fallback para atendimento humano
  - `business-strategy/`
    - `fim-do-chatbot.md`: conceitos de automação invisível
    - `o-segredo-do-contrato.md`: estratégia de retenção e valor no modelo B2B
  - `generated/`
    - Saídas visuais geradas (PNG/SVG), geralmente ignoradas em controle de versão
  - `README.md`
    - Índice principal da documentação

## Uso rápido

1. Levantar a infraestrutura local:

```bash
make infra-up
```

2. Visualizar logs:

```bash
make infra-logs
```

3. Parar a infraestrutura:

```bash
make infra-down
```

4. Reiniciar e purgar volumes de dados:

```bash
make infra-clean
```

## Requisitos

- Docker
- Docker Compose
- Python (apenas se for gerar a topologia com `docs/infrastructure/generate_topology.py`)

## Observações

- O projeto está organizado como um repositório de documentação e suporte de infraestrutura, mais do que uma aplicação completa pronta para produção.
- A infraestrutura local usa volumes Docker para persistência de dados entre reinicializações.
- Os diagramas e artefatos gerados podem ser encontrados em `docs/generated/`.
