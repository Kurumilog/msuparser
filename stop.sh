#!/bin/bash

# Скрипт для остановки системы
# Использование: ./stop.sh

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="$PROJECT_DIR/.pids"

echo "🛑 Остановка MSU Timetable Bot"
echo "=============================="

if [ ! -f "$PID_FILE" ]; then
    echo "❌ Процессы не найдены (нет файла $PID_FILE)"
    exit 1
fi

PIDs=$(cat "$PID_FILE")

if [ -z "$PIDs" ]; then
    echo "❌ Нет активных процессов"
    exit 1
fi

for pid in $PIDs; do
    if kill -0 "$pid" 2>/dev/null; then
        echo "🔌 Остановка процесса $pid..."
        kill "$pid"
        
        # Ждем завершения процесса
        for i in {1..10}; do
            if ! kill -0 "$pid" 2>/dev/null; then
                echo "   ✅ Процесс $pid остановлен"
                break
            fi
            sleep 1
        done
        
        # Если процесс все еще работает, убиваем с SIGKILL
        if kill -0 "$pid" 2>/dev/null; then
            echo "   ⚠️  Принудительная остановка процесса $pid..."
            kill -9 "$pid"
            echo "   ✅ Процесс $pid убит"
        fi
    else
        echo "⚠️  Процесс $pid не найден"
    fi
done

# Очищаем файл PID
rm "$PID_FILE"

echo ""
echo "✅ Система остановлена"
echo ""
