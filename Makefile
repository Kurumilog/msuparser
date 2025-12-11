.PHONY: all build test clean install help deploy

# Переменные
BINARY_NAME=test_parser
MAIN_BINARY=main
GO=go
GOFLAGS=-v

all: build build-main

# Установка зависимостей
install:
	@echo "Установка зависимостей..."
	$(GO) get github.com/PuerkitoBio/goquery
	$(GO) mod tidy

# Сборка тестового парсера
build:
	@echo "Сборка парсера..."
	$(GO) build -o $(BINARY_NAME) test_parser.go parser.go

# Сборка основного бота
build-main:
	@echo "Сборка бота..."
	$(GO) build -o $(MAIN_BINARY) main.go parser.go

# Запуск парсера
test:
	@echo "Запуск парсера..."
	./$(BINARY_NAME)

# Запуск бота (требует schedule.json)
run:
	@echo "Запуск бота..."
	./$(MAIN_BINARY)

# Очистка
clean:
	@echo "Очистка..."
	rm -f $(BINARY_NAME) $(MAIN_BINARY)
	rm -f schedule.json

# Проверка кода
lint:
	@echo "Проверка кода..."
	$(GO) vet ./...
	$(GO) fmt ./...

# Развертывание на сервер (требует настроенный SSH)
deploy:
	@echo "Развертывание на сервер..."
	@if [ -z "$(SERVER)" ]; then \
		echo "❌ Укажите SERVER=user@host"; \
		exit 1; \
	fi
	rsync -avz --exclude='.git' --exclude='logs' --exclude='venv' \
		. $(SERVER):~/msuparser/
	ssh $(SERVER) 'cd ~/msuparser && make build build-main && sudo systemctl restart msuparser-bot'

# Информация
help:
	@echo "Доступные команды:"
	@echo "  make install      - Установить зависимости"
	@echo "  make build        - Собрать парсер"
	@echo "  make build-main   - Собрать бота"
	@echo "  make test         - Запустить парсер"
	@echo "  make run          - Запустить бота"
	@echo "  make clean        - Удалить бинарники"
	@echo "  make lint         - Проверить код"
	@echo "  make deploy       - Деплой на сервер (SERVER=user@host)"
	@echo "  make all          - Собрать всё (парсер + бот)"
