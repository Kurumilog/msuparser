# Установка MSU Timetable Bot на Ubuntu Server

Полное руководство по развертыванию бота расписания МГУ на удаленном сервере Ubuntu.

## Требования

- Ubuntu 20.04+ (или Debian 11+)
- Go 1.24+
- Доступ по SSH с правами sudo
- Telegram бот токен

## Быстрая установка (5 минут)

### 1. Подключение к серверу

```bash
ssh ubuntu@your-server-ip
```

### 2. Установка Go

```bash
# Скачиваем последнюю версию Go
wget https://go.dev/dl/go1.24.0.linux-amd64.tar.gz

# Удаляем старую версию (если есть)
sudo rm -rf /usr/local/go

# Распаковываем
sudo tar -C /usr/local -xzf go1.24.0.linux-amd64.tar.gz

# Добавляем в PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Проверяем установку
go version
```

### 3. Клонирование проекта

```bash
cd ~
git clone https://github.com/Kurumilog/msuparser.git
cd msuparser
```

### 4. Настройка конфигурации

```bash
# Копируем пример конфига
cp config.example.py config.py

# Редактируем конфиг
nano config.py
```

Заполните:
```python
BOT_TOKEN = "your-telegram-bot-token"
USER_ID = "your-telegram-user-id"
NOTIFICATION_MINUTES = 15
```

**Как узнать USER_ID:**
1. Напишите боту [@userinfobot](https://t.me/userinfobot)
2. Скопируйте ваш ID

### 5. Сборка приложения

```bash
# Устанавливаем зависимости
go get github.com/PuerkitoBio/goquery
go mod tidy

# Собираем парсер
go build -o test_parser test_parser.go parser.go

# Собираем бота
go build -o main main.go parser.go
```

### 6. Тестирование

```bash
# Проверяем парсер
./test_parser

# Должно появиться:
# ✅ Найдено занятий: XX
# 💾 Расписание сохранено в schedule.json
```

### 7. Установка сервисов

```bash
# Копируем файлы сервисов
sudo cp msuparser-bot.service /etc/systemd/system/
sudo cp msuparser-update.service /etc/systemd/system/
sudo cp msuparser-update.timer /etc/systemd/system/

# Редактируем пути в сервисах (если нужно)
sudo nano /etc/systemd/system/msuparser-bot.service
sudo nano /etc/systemd/system/msuparser-update.service

# Перезагружаем systemd
sudo systemctl daemon-reload
```

### 8. Запуск сервисов

```bash
# Включаем автозапуск
sudo systemctl enable msuparser-bot.service
sudo systemctl enable msuparser-update.timer

# Запускаем бота
sudo systemctl start msuparser-bot.service

# Запускаем таймер обновления расписания (раз в 3 дня)
sudo systemctl start msuparser-update.timer

# Проверяем статус
sudo systemctl status msuparser-bot.service
sudo systemctl status msuparser-update.timer
```

## Управление сервисами

### Проверка статуса

```bash
# Статус бота
sudo systemctl status msuparser-bot

# Логи бота
sudo journalctl -u msuparser-bot -f

# Логи парсера
sudo journalctl -u msuparser-update -f
```

### Перезапуск

```bash
# Перезапуск бота
sudo systemctl restart msuparser-bot

# Ручной запуск парсера
sudo systemctl start msuparser-update
```

### Остановка

```bash
# Остановка бота
sudo systemctl stop msuparser-bot

# Остановка таймера обновления
sudo systemctl stop msuparser-update.timer
```

### Просмотр логов

```bash
# Последние 100 строк логов бота
sudo journalctl -u msuparser-bot -n 100

# Логи за сегодня
sudo journalctl -u msuparser-bot --since today

# Следить за логами в реальном времени
sudo journalctl -u msuparser-bot -f
```

## Обновление

```bash
cd ~/msuparser

# Получаем последние изменения
git pull

# Пересобираем
go build -o test_parser test_parser.go parser.go
go build -o main main.go parser.go

# Перезапускаем
sudo systemctl restart msuparser-bot
```

## Расписание обновлений

По умолчанию парсер запускается раз в 3 дня в 03:00.

Изменить расписание можно в файле `msuparser-update.timer`:

```bash
sudo nano /etc/systemd/system/msuparser-update.timer
```

```ini
[Timer]
OnCalendar=*-*-* 03:00:00  # Каждый день в 3:00
# OnCalendar=Mon,Thu 03:00:00  # Пн и Чт в 3:00
# OnCalendar=daily  # Каждый день
```

После изменения:
```bash
sudo systemctl daemon-reload
sudo systemctl restart msuparser-update.timer
```

## Структура проекта

```
msuparser/
├── main.go                      # Telegram бот
├── parser.go                    # Парсер расписания
├── test_parser.go               # Тестовый запуск парсера
├── config.py                    # Конфигурация (BOT_TOKEN, USER_ID)
├── schedule.json                # Кэш расписания
├── msuparser-bot.service        # Systemd сервис бота
├── msuparser-update.service     # Systemd сервис обновления
└── msuparser-update.timer       # Таймер обновления расписания
```

## Особенности работы

### Уведомления

- **Обычные пары**: Уведомление за 15 минут до начала
- **Дистанционные пары**: Одно уведомление утром в 8:00 со списком всех дистанционных пар на день
- **3 пара**: Уведомление за 45 минут (т.к. после обеда)

### Дистанционные пары

Бот автоматически определяет дистанционные пары по ключевым словам в названии аудитории:
- `дистанц`
- `виртуал`

Вместо отправки отдельного уведомления перед каждой дистанционной парой, бот отправляет одно сообщение утром:

```
📱 Напоминание

У вас сегодня дистанционные пары:

• 1 пара (09:00-10:30)
  📚 Международное право [Сем]
  👨‍🏫 Пименова Софья Дмитриевна
  
• 2 пара (10:45-12:15)
  📚 Земельное право [Лк]
  👨‍🏫 Старова Екатерина Владимировна
```

## Мониторинг

### Проверка работоспособности

```bash
# Проверяем, работает ли бот
sudo systemctl is-active msuparser-bot

# Проверяем, включен ли автозапуск
sudo systemctl is-enabled msuparser-bot

# Проверяем таймер обновления
sudo systemctl list-timers | grep msuparser
```

### Настройка алертов

Можно настроить уведомления при падении сервиса через systemd:

```bash
sudo nano /etc/systemd/system/msuparser-bot.service
```

Добавить:
```ini
[Unit]
OnFailure=status-email@%n.service
```

## Резервное копирование

```bash
# Создаем резервную копию конфигурации
cp ~/msuparser/config.py ~/msuparser-config-backup.py

# Создаем резервную копию schedule.json
cp ~/msuparser/schedule.json ~/msuparser-schedule-backup.json
```

## Безопасность

### Ограничение доступа к config.py

```bash
chmod 600 ~/msuparser/config.py
```

### Использование переменных окружения (опционально)

Вместо `config.py` можно использовать переменные окружения:

```bash
sudo nano /etc/systemd/system/msuparser-bot.service
```

Добавить:
```ini
[Service]
Environment="BOT_TOKEN=your-token"
Environment="USER_ID=your-id"
```

## Troubleshooting

### Бот не запускается

```bash
# Проверяем логи
sudo journalctl -u msuparser-bot -n 50

# Проверяем конфиг
cat ~/msuparser/config.py

# Проверяем schedule.json
ls -lh ~/msuparser/schedule.json
```

### Парсер не работает

```bash
# Запускаем вручную
cd ~/msuparser
./test_parser

# Проверяем права доступа
ls -lh test_parser
chmod +x test_parser
```

### Уведомления не приходят

1. Проверьте BOT_TOKEN и USER_ID в config.py
2. Убедитесь, что вы написали боту `/start`
3. Проверьте часовой пояс: `timedatectl`

### Изменение часового пояса

```bash
sudo timedatectl set-timezone Europe/Minsk
```

## Настройка автоматических обновлений

```bash
# Создаем скрипт обновления
nano ~/update-msuparser.sh
```

```bash
#!/bin/bash
cd ~/msuparser
git pull
go build -o test_parser test_parser.go parser.go
go build -o main main.go parser.go
sudo systemctl restart msuparser-bot
```

```bash
# Делаем исполняемым
chmod +x ~/update-msuparser.sh

# Добавляем в cron (каждое воскресенье в 4:00)
crontab -e
```

Добавить:
```
0 4 * * 0 /home/ubuntu/update-msuparser.sh >> /home/ubuntu/msuparser-update.log 2>&1
```

## Контакты и поддержка

- GitHub: https://github.com/Kurumilog/msuparser
- Issues: https://github.com/Kurumilog/msuparser/issues

## Лицензия

См. LICENSE файл в репозитории.
