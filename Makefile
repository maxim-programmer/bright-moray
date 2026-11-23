# Управление администраторами и вспомогательные команды.
#
#   make admin-create   создать администратора (или повысить существующего пользователя)
#   make admin-delete   откатить: удалить созданного администратора или снять роль admin
#
# Команды выполняются в контейнере backend; контейнер db при необходимости поднимется сам.
# Для dev-режима:
#   make admin-create COMPOSE="docker compose -f docker-compose.yml -f docker-compose.dev.yml"

COMPOSE ?= docker compose

.PHONY: admin-create admin-delete admin-create-local admin-delete-local

admin-create:
	$(COMPOSE) run --rm backend /admin create

admin-delete:
	$(COMPOSE) run --rm backend /admin delete

# Без Docker для приложения: нужен Go и БД на localhost:5432 (docker-compose.dev.yml открывает порт).
# Параметры подключения берутся из .env (POSTGRES_DB, POSTGRES_USER, POSTGRES_PASSWORD).
admin-create-local:
	@set -a; . ./.env; set +a; cd backend && DB_HOST=$${DB_HOST:-localhost} DB_NAME=$$POSTGRES_DB DB_USER=$$POSTGRES_USER DB_PASSWORD=$$POSTGRES_PASSWORD go run ./cmd/admin create

admin-delete-local:
	@set -a; . ./.env; set +a; cd backend && DB_HOST=$${DB_HOST:-localhost} DB_NAME=$$POSTGRES_DB DB_USER=$$POSTGRES_USER DB_PASSWORD=$$POSTGRES_PASSWORD go run ./cmd/admin delete
