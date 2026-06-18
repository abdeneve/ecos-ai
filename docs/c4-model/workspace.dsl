workspace "Ecosistema SaaS AI" "Arquitectura de Orquestación Headless y Handoff Asíncrono" {

    model {
        customer = person "Cliente Final" "Usuario que interactúa a través de WhatsApp." "Cliente"
        agent = person "Agente Humano" "Operador de soporte o ventas del Tenant B2B." "Agente"
        
        whatsappAPI = softwareSystem "Meta WhatsApp API" "Infraestructura externa que entrega webhooks." "Externo"
        llmProvider = softwareSystem "Proveedor de LLM" "Servicio externo de inferencia (Vertex AI / OpenAI)." "Externo"

        enterpriseSaaS = softwareSystem "Plataforma SaaS de Orquestación IA" "Orquesta flujos conversacionales y gestiona el handoff." "Sistema Central" {
            
            webApp = container "Frontend Web App" "Panel de control del Tenant y bandeja de entrada en tiempo real." "Next.js" "Frontend"
            ingressGateway = container "Ingress Webhook Edge" "Servicio ultra-ligero para recibir y encolar webhooks de Meta." "Go" "Backend"
            eventBroker = container "Event Streaming" "Buffer central de eventos distribuidos para backpressure." "Kafka" "Mensajeria"
            agentEngine = container "Agentic Orchestrator" "Pool de workers asíncronos que ejecutan la máquina de estados." "Go" "Backend"
            cacheCluster = container "Distributed Cache" "Estado de sesión, idempotencia y locking distribuido." "Redis" "BaseDeDatos"
            mainStorage = container "Main Persistence" "Historial inmutable particionado para evitar hot-partitions." "ScyllaDB" "BaseDeDatos"
        }

        # Relaciones
        customer -> whatsappAPI "Envía y recibe mensajes"
        whatsappAPI -> ingressGateway "Entrega Webhooks (HTTPS/POST)"
        ingressGateway -> cacheCluster "Verifica idempotencia (TTL 24h)"
        ingressGateway -> eventBroker "Produce eventos (Partición por tenant)"
        eventBroker -> agentEngine "Consume eventos secuencialmente"
        agentEngine -> cacheCluster "Lee/Actualiza estado de sesión"
        agentEngine -> mainStorage "Lee/Escribe historial conversacional"
        agentEngine -> llmProvider "Ejecuta bucle agéntico"
        agentEngine -> whatsappAPI "Envía respuesta final"
        agentEngine -> cacheCluster "Publica eventos en Pub/Sub"
        
        agent -> webApp "Opera el Inbox"
        webApp -> cacheCluster "WebSockets (Suscripción Pub/Sub)"
        webApp -> mainStorage "Consulta historial paginado"
    }

    views {
        systemContext enterpriseSaaS "Visión_Contexto" {
            include *
            autoLayout tb
            description "Diagrama de Contexto del Sistema (Nivel 1)."
        }

        container enterpriseSaaS "Visión_Contenedores" {
            include *
            autoLayout tb
            description "Arquitectura de Contenedores y Flujo Headless (Nivel 2)."
        }

        # Enlace al archivo de estilos visuales
        theme theme.json
    }
}