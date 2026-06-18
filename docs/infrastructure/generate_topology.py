# docs/infrastructure/generate_topology.py

import os
from diagrams import Diagram, Cluster, Edge
from diagrams.onprem.queue import Kafka
from diagrams.onprem.inmemory import Redis
from diagrams.onprem.database import Scylla
from diagrams.custom import Custom
from diagrams.programming.language import Go
from diagrams.programming.framework import React

# 1. Configuración de Rutas (Ejecución Headless)
# Asegura que el output vaya directo a docs/generated sin importar desde dónde se ejecute
base_dir = os.path.dirname(__file__)
output_path = os.path.join(base_dir, '..', 'generated', 'topology')

# 2. Configuración Visual Mejorada
# Fondo oscuro con contraste alto, componentes rectangulares y nodos espaciados para mayor legibilidad
graph_attr = {
    "bgcolor": "#090B0F",
    "fontcolor": "#F8FAFC",
    "nodesep": "1.2",
    "ranksep": "1.8",
    "pad": "0.5",
    "splines": "ortho"
}

# Nodos con relleno, bordes suaves y texto claro
node_attr = {
    "shape": "rect",
    "style": "filled,rounded",
    "fillcolor": "#111418",
    "color": "#2E3A4B",
    "fontcolor": "#F8FAFC",
    "fontsize": "16",
    "fontname": "Helvetica-Bold",
    "margin": "0.35,0.20"
}

# Conexiones más suaves, texto legible y anchura consistente
edge_attr = {
    "color": "#66D9FF",
    "fontcolor": "#C7F0FF",
    "fontsize": "12",
    "fontname": "Helvetica",
    "arrowsize": "0.8",
    "penwidth": "1.8"
}

# 3. Definición de la Arquitectura
with Diagram(
    name="Infraestructura de Orquestación Asíncrona",
    show=False,                     # Ejecución 100% silenciosa/headless
    filename=output_path,
    graph_attr=graph_attr,
    node_attr=node_attr,
    edge_attr=edge_attr,
    direction="LR"                  # Flujo de Izquierda a Derecha
):
    
    # Capa 1: Borda e Fronteira
    with Cluster("Borda & Fronteira", graph_attr={"bgcolor": "#10131A", "fontcolor": "#F8FAFC", "color": "#2F3D52", "style": "rounded"}):
        wa_api = Custom("Meta API\n(WhatsApp)", "https://upload.wikimedia.org/wikipedia/commons/6/6b/WhatsApp.svg")
        dashboard = React("Next.js\n(Painel Handoff)")

    # Capa 2: Ingestão e Proteção de Picos
    with Cluster("Camada de Ingestão", graph_attr={"bgcolor": "#12161F", "fontcolor": "#F8FAFC", "color": "#384158", "style": "rounded"}):
        ingress = Go("Ingress\nWebhook")
        broker = Kafka("Kafka\n(Event Stream)")

    # Capa 3: Core Headless e Inteligência
    with Cluster("Camada Core (Headless)", graph_attr={"bgcolor": "#12161F", "fontcolor": "#F8FAFC", "color": "#0EA5E9", "style": "rounded"}):
        worker = Go("Agent Worker\n(Orquestrador)")
        cache = Redis("Redis Cluster\n(Estado & Pub/Sub)")
        llm = Custom("LLM API\n(Vertex/OpenAI)", "https://upload.wikimedia.org/wikipedia/commons/0/04/ChatGPT_logo.svg")

    # Capa 4: Persistência
    with Cluster("Camada de Persistência", graph_attr={"bgcolor": "#10131A", "fontcolor": "#F8FAFC", "color": "#2F3D52", "style": "rounded"}):
        db = Scylla("ScyllaDB\n(Histórico)")

    # 4. Trazado del Flujo de Datos y Eventos
    
    # Flujo de datos principal
    wa_api >> Edge(label="1. Webhook POST", color="#66D9FF") >> ingress
    ingress >> Edge(label="2. Valida\nIdempotência", style="dashed", color="#FB7185", fontcolor="#FB7185") >> cache
    ingress >> Edge(label="3. Produz\nEvento", color="#66D9FF") >> broker

    broker >> Edge(label="4. Consome\nFila", color="#66D9FF") >> worker
    worker >> Edge(label="5. Lê/Grava\nEstado", style="dashed", color="#66D9FF") >> cache
    worker >> Edge(label="6. Agente /\nLLM", color="#66D9FF") >> llm

    worker >> Edge(label="7. Grava\nHistórico", color="#66D9FF") >> db
    worker >> Edge(label="8. Envia\nResposta", color="#66D9FF") >> wa_api

    cache >> Edge(label="WebSockets\n(Pub/Sub)", style="dotted", color="#66D9FF") >> dashboard
    dashboard >> Edge(label="Consultas\nPaginadas", style="dashed", color="#94A3B8") >> db

print(f"✅ Topologia compilada com sucesso. Arquivo gerado em: {output_path}.png")