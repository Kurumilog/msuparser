#!/bin/bash

# Простой скрипт для запуска всей системы в фоне
# Использование: ./start.sh

set -e

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV_DIR="$PROJECT_DIR/venv"
BOT_BINARY="$PROJECT_DIR/bot"
PARSER="$PROJECT_DIR/parser_full.py"
LOG_DIR="$PROJECT_DIR/logs"
PID_FILE="$PROJECT_DIR/.pids"

# Создаем директорию логов
mkdir -p "$LOG_DIR"

echo "🚀 MSU Timetable Bot - Запуск системы"
echo "===================================="

# Проверяем наличие виртуального окружения
if [ ! -d "$VENV_DIR" ]; then
    echo "❌ Виртуальное окружение не найдено!"
    echo "💡 Создай его: python -m venv venv && source venv/bin/activate && pip install -r requirements.txt"
    exit 1
fi

# Проверяем наличие бинарника Go
if [ ! -f "$BOT_BINARY" ]; then
    echo "❌ Go бот не скомпилирован!"
    echo "💡 Скомпилируй его: go build -o bot main.go"
    exit 1
fi

# Проверяем наличие schedule.json
if [ ! -f "$PROJECT_DIR/schedule.json" ]; then
    echo "📥 Расписание еще не загружено, запускаю парсер..."
    source "$VENV_DIR/bin/activate"
    python "$PARSER"
    deactivate
fi

# Удаляем старые PID файлы мертвых процессов
if [ -f "$PID_FILE" ]; then
    OLD_PIDs=$(cat "$PID_FILE" 2>/dev/null || echo "")
    for pid in $OLD_PIDs; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid"
            echo "🛑 Остановлен процесс $pid"
        fi
    done
    rm "$PID_FILE"
fi

# Запускаем парсер в фоне с периодическим обновлением (раз в 3 дня)
echo "📅 Запускаю фоновый парсер (обновление раз в 3 дня)..."
{
    while true; do
        # Ждем 3 дня (259200 секунд)
        sleep 259200
        source "$VENV_DIR/bin/activate"
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Обновляю расписание..." >> "$LOG_DIR/parser_bg.log"
        python "$PARSER" >> "$LOG_DIR/parser_bg.log" 2>&1
        deactivate
    done
} &
PARSER_PID=$!
echo "$PARSER_PID" >> "$PID_FILE"
echo "   ✅ Парсер запущен (PID: $PARSER_PID)"

# Даем парсеру время завершить первый запуск если schedule.json был пустой
sleep 2

# Запускаем Go бота
echo "🤖 Запускаю Go бота..."
cd "$PROJECT_DIR"
nohup "$BOT_BINARY" >> "$LOG_DIR/bot.log" 2>&1 &
BOT_PID=$!
echo "$BOT_PID" >> "$PID_FILE"
echo "   ✅ Бот запущен (PID: $BOT_PID)"

echo ""
echo "===================================="
echo "✅ Система запущена!"
echo ""
echo "📋 Логи:"
echo "   • Бот:         tail -f $LOG_DIR/bot.log"
echo "   • Парсер:      tail -f $LOG_DIR/parser_bg.log"
echo ""
echo "🛑 Для остановки:"
echo "   ./stop.sh"
echo ""
