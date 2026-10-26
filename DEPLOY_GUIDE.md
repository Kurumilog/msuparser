# Руководство по развертыванию MSU Timetable Bot

Полное руководство по установке, обновлению и поддержке Telegram-бота расписания МГУ ВШГА на удаленном сервере (Ubuntu/Debian).

---

## Требования

- Ubuntu 20.04+ или Debian 11+
- Go 1.21+ (рекомендуется 1.24+)
- Доступ по SSH к серверу
- Telegram бот токен от [@BotFather](https://t.me/BotFather)
- Ваш Telegram ID от [@userinfobot](https://t.me/userinfobot)

---

## Быстрая установка

### 1. Подключение к серверу

```bash
ssh root@your-server-ip
```

### 2. Установка Go (если еще не установлен)

```bash
# Скачиваем стабильную версию Go
wget https://go.dev/dl/go1.24.0.linux-amd64.tar.gz

# Удаляем старую версию и распаковываем
rm -rf /usr/local/go
tar -C /usr/local -xzf go1.24.0.linux-amd64.tar.gz
rm go1.24.0.linux-amd64.tar.gz

# Добавляем в PATH (если отсутствует)
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Проверяем
go version
```

### 3. Подготовка каталога проекта

```bash
mkdir -p /root/msuparser
cd /root/msuparser
```

### 4. Настройка конфигурации (`config.json`)

Создайте файл `config.json`:

```json
{
  "BOT_TOKEN": "ваш-токен-от-BotFather",
  "USER_ID": "ваш-числовой-telegram-id",
  "NOTIFICATION_MINUTES": 15
}
```

Ограничьте права доступа:
```bash
chmod 600 config.json
```

---

## Перенос файлов на сервер

Поскольку на сервере репозиторий развернут без `.git`, файлы отправляются с локальной машины через `scp` или `rsync`:

### С локального компьютера:

```bash
cd /Users/kurumi/code/msuparser

# Отправка исходного кода (конфиги и локальные json исключены)
scp main.go gifs.go parser.go changes.go test_parser.go Makefile deploy.sh root@5.253.188.95:/root/msuparser/
```

---

## Сборка и запуск на сервере

### 1. Установка зависимостей и сборка

```bash
cd /root/msuparser

# Зависимости
go get github.com/PuerkitoBio/goquery
go mod tidy

# Сборка парсера и бота
go build -o test_parser test_parser.go parser.go
go build -o main main.go parser.go gifs.go changes.go
```

### 2. Проверка работы парсера

```bash
./test_parser
```

Успешный вывод:
```
📚 Получение расписания с tt.audit.msu.ru...
✅ Найдено занятий: XX
💾 Расписание сохранено в schedule.json
```

### 3. Настройка systemd-сервиса бота

Создайте или отредактируйте unit-файл `/etc/systemd/system/msuparser-bot.service`:

```ini
[Unit]
Description=MSU Timetable Bot (Go)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/root/msuparser
ExecStart=/root/msuparser/main
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=msuparser-bot

[Install]
WantedBy=multi-user.target
```

### 4. Отключение устаревшего таймера обновления

> **Важно**: Бот теперь выполняет **постоянную фоновую индексацию сам** (каждые 15 минут днем и каждый час ночью), сравнивает изменения с прошлым снимком и присылает детальные push-уведомления. Внешний systemd-таймер обновления больше не нужен и должен быть отключен, чтобы не перезаписывать `schedule.json` в обход механизма сравнения.

```bash
systemctl disable --now msuparser-update.timer 2>/dev/null || true
```

### 5. Запуск бота

```bash
systemctl daemon-reload
systemctl enable msuparser-bot
systemctl restart msuparser-bot
```

### 6. Проверка статуса и логов

```bash
systemctl is-active msuparser-bot
journalctl -u msuparser-bot -f
```

---

## Как устроена работа бота

### 1. Постоянная индексация и оповещения об изменениях
- **Интервал**: каждые 15 минут в дневное время (07:00–23:00 МСК) и раз в час ночью.
- **Окно отслеживания**: парсинг ведется на 14 дней вперед, push-уведомления об изменениях высылаются для событий ближайших 7 дней.
- **Типы изменений**:
  - `🟥 + Добавлена пара`
  - `🟩 − Отменена пара`
  - `🔄 Перенос пары` (как внутри дня, так и с другого дня)
  - `🟥 Замена предмета` в том же временном слоте
  - `🔄 Изменение аудитории или смена преподавателя`
- **Оформление**: моноширинное выравнивание времени (`1 пара 09:00–10:30`), аудитория курсивом, преподаватель отдельной строкой.
- **Защита от сбоев**: если сайт временно вернул пустую страницу, бот не считает это отменой занятий вплоть до 4 сбоев подряд. Снимок хранится в `schedule.json` и `schedule_meta.json`.

### 2. Обычные уведомления перед занятиями
- За 15 минут до начала каждой пары (для 3-й пары — за 45 минут).

### 3. Утреннее напоминание в 08:00
- Отправляет итоговый список всех пар на день (время, предмет, аудитория, преподаватель).
- Настраивается прямо в боте через команду `/settings` → **«Утреннее напоминание о парах»**:
  - `✅ Дистант` — включить в дни, когда все пары дистанционные.
  - `✅ Очно` — включить в обычные учебные дни с очными занятиями.

### 4. Персональные медиа (гифки, стикеры, видео)
- Команда `/gifs` позволяет загружать медиа и привязывать их к слотам: `🌅 Утро`, `🌙 Вечер`, `🔔 Уведомления`, `📱 Команды`.
- Данные медиа и персональные настройки хранятся в `gifs.json` и **никогда не перезаписываются при деплое**.

---

## Обновление бота на сервере

Безопасное обновление без потери настроек и расписания:

```bash
cd /root/msuparser

# 1. Создаем резервную копию
mkdir -p backups/deploy-$(date +%Y%m%d-%H%M%S)
cp -a main main.go parser.go gifs.go changes.go config.json schedule.json schedule_meta.json gifs.json backups/deploy-*/ 2>/dev/null || true

# 2. Компилируем во временный бинарник
go build -o main.new main.go parser.go gifs.go changes.go

# 3. Атомарно заменяем бинарник и перезапускаем
install -m 755 main.new main && rm -f main.new
systemctl restart msuparser-bot

# 4. Проверяем логи
sleep 2
systemctl is-active msuparser-bot
journalctl -u msuparser-bot -n 25 --no-pager
```

---

## Файловая структура на сервере

```
/root/msuparser/
├── main                    # Скомпилированный бинарник бота
├── test_parser             # Вспомогательный CLI парсер
├── main.go                 # Основной цикл бота, планировщик и команды
├── parser.go               # HTTP парсер tt.audit.msu.ru (14 дней)
├── gifs.go                 # Управление гифками и меню /settings
├── changes.go              # Индексация, дифф и push-уведомления
├── config.json             # Токен бота и ID пользователя (не перезаписывать!)
├── schedule.json           # Актуальный кэш расписания
├── schedule_meta.json      # Метаданные диапазона последнего снимка
├── gifs.json               # Пользовательские гифки и настройки времени
├── Makefile                # Скрипты локальной сборки
├── deploy.sh               # Скрипт автоматизированного развертывания
└── backups/                # Автоматические резервные копии
```

---

## Диагностика и решение проблем

### Бот не запускается
```bash
journalctl -u msuparser-bot -n 50 --no-pager
```
- Проверьте валидность `config.json`: `jq . config.json`
- Проверьте права на исполняемый файл: `ls -la main`

### Проверка часового пояса
Время на сервере должно быть синхронизировано по Москве:
```bash
timedatectl
# Если часовой пояс отличается:
timedatectl set-timezone Europe/Moscow
```

### Откат к предыдущей версии
В случае любых проблем восстановите предыдущее состояние из папки бэкапов:
```bash
cd /root/msuparser
cp -a backups/pre-deploy/* .
systemctl restart msuparser-bot
```
