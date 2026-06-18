# ==============================================================================
# Orquestación de Infraestructura Local (WSL2 / Ubuntu)
# ==============================================================================

.PHONY: infra-up infra-down infra-logs infra-clean

# Levanta la infraestructura silenciosamente en background
infra-up:
	docker compose up -d
	@echo "✅ Infraestructura Core Activa (Redpanda, Redis, ScyllaDB)"

# Derruba los contenedores sin destruir los datos persistidos
infra-down:
	docker compose down
	@echo "🛑 Infraestructura Core Detenida"

# Stream de logs en tiempo real
infra-logs:
	docker compose logs -f

# Hard Reset: Derruba y purga todos los volúmenes de datos
infra-clean:
	docker compose down -v
	@echo "🔥 Infraestructura destruida (Volúmenes purgados)"