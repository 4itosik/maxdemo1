MAXAPI  ?= ../maxapi
CONFIG  ?= config.yaml
DB      ?= max-mock.db
GOOS    ?= linux
GOARCH  ?= amd64

.PHONY: build test run e2e smoke release sync-specs clean reset

build:
	go build -o max-mock ./cmd/max-mock

test:
	go test ./...

# Запуск с конфигом, если он есть, и на значениях по умолчанию, если его нет:
# config.yaml не хранится в репозитории, и требовать его для первого запуска
# незачем.
run: build
	@if [ -f "$(CONFIG)" ]; then \
		echo "max-mock: конфиг $(CONFIG)"; ./max-mock -config "$(CONFIG)"; \
	else \
		echo "max-mock: конфига $(CONFIG) нет, запускаюсь на значениях по умолчанию"; ./max-mock; \
	fi

e2e:
	go test ./e2e/ -v

smoke: build
	./scripts/smoke.sh

# Статический бинарник для переноса в закрытый контур.
# CGO отключён явно: SQLite здесь на чистом Go, и без cgo сборка не тянет
# системных библиотек — файл запускается на любом хосте нужной архитектуры.
release:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags="-s -w" -o max-mock-$(GOOS)-$(GOARCH) ./cmd/max-mock
	@echo "готово: max-mock-$(GOOS)-$(GOARCH)"

# Обновить OpenAPI-артефакты из репозитория maxapi (источник правды контракта).
sync-specs:
	cp $(MAXAPI)/tsp-output/@typespec/openapi3/openapi.MaxBotApi.yaml api/
	cp $(MAXAPI)/tsp-output/@typespec/openapi3/openapi.MaxBotWebhook.yaml api/
	go test ./internal/specs/ ./e2e/

# clean убирает только собранные бинарники.
#
# Состояние мока (база и загруженные файлы) здесь НЕ трогается намеренно:
# в нём живут боты с выданными токенами, на которые уже настроены стенды,
# и переписка тестировщиков. Потерять это из-за цели с названием «clean» —
# слишком дорого. Для сброса состояния есть отдельная цель `reset`.
clean:
	rm -f max-mock max-mock-*-*

# reset удаляет состояние мока: боты, их токены, диалоги, загруженные файлы.
# Требует явного подтверждения, потому что отменить это нельзя, а стенды
# после сброса придётся перенастраивать на новые токены.
reset:
	@if [ "$(CONFIRM)" != "yes" ]; then \
		echo "reset удалит базу $(DB) и каталог blobs — боты, токены, диалоги и файлы."; \
		echo "Стенды после этого придётся перенастроить на новые токены."; \
		echo "Если это то, что нужно: make reset CONFIRM=yes"; \
		exit 1; \
	fi
	rm -f $(DB) $(DB)-wal $(DB)-shm
	rm -rf blobs
	@echo "состояние мока сброшено"
