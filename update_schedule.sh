#!/bin/bash

# Скрипт для обновления расписания
# Запускается раз в 3 дня через systemd timer

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV_DIR="$PROJECT_DIR/venv"
PARSER="$PROJECT_DIR/parser_full.py"
LOG_FILE="$PROJECT_DIR/logs/parser.log"

# Создаем директорию логов если её нет
mkdir -p "$PROJECT_DIR/logs"

# Логирование
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

log "🔄 Запуск обновления расписания..."

# Активируем venv
if [ -f "$VENV_DIR/bin/activate" ]; then
    source "$VENV_DIR/bin/activate"
else
    log "❌ Виртуальное окружение не найдено в $VENV_DIR"
    exit 1
fi

# Запускаем парсер
log "📥 Запуск parser_full.py..."
if python "$PARSER"; then
    log "✅ Парсер успешно завершил работу"
else
    log "❌ Ошибка при запуске парсера (код выхода: $?)"
    exit 1
fi

log "✔️ Обновление расписания завершено"
