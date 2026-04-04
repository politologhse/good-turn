# Good TURN

Прокси для обхода блокировок через TURN-серверы VK-звонков.

ТСПУ не трогает трафик к VK TURN (иначе сломаются звонки). Good TURN маскирует интернет-трафик под VK-видеозвонок и пробрасывает его на ваш VPS.

> Только для учебных целей.

## Что происходит внутри

```
Ваш ПК                          VK TURN сервер              Ваш VPS
┌──────────┐    DTLS/STUN        (белый список)     UDP      ┌──────────┐
│ Браузер  ├──► Hysteria2 ──► good-turn client ═══════════► good-turn  │
│          │    SOCKS5:1080   127.0.0.1:9000                 server    ├──► Интернет
└──────────┘                                                 :56000    │
                                                             ↓         │
                                                             Hysteria2 │
                                                             :443      │
                                                             └──────────┘
```

## Что нужно

1. **VPS за границей** (любой Linux)
2. **VK-ссылка на звонок** — откройте [vk.com/calls](https://vk.com/calls), создайте звонок, скопируйте ссылку. Не нажимайте «завершить для всех». Ссылка живёт, пока звонок не завершён.

## Настройка сервера (VPS)

SSH на VPS и выполните:

```bash
curl -fsSL https://raw.githubusercontent.com/politologhse/good-turn/main/setup-server.sh | bash -s -- -pass ваш-пароль
```

Скрипт сам поставит Hysteria2, сгенерирует сертификат, создаст systemd-сервисы и выведет **строку конфигурации**:

```
gt://eyJhIjoiMTg1LjEuMi4zOjU2MDAwIiwicCI6...
```

Сохраните её — передадите на клиент.

<details>
<summary>Ручная установка (без curl | bash)</summary>

```bash
# 1. Hysteria2
bash <(curl -fsSL https://get.hy2.sh/)

# Самоподписанный сертификат
openssl req -x509 -nodes -newkey ec:<(openssl ecparam -name prime256v1) \
  -keyout /etc/hysteria/key.pem -out /etc/hysteria/cert.pem \
  -subj "/CN=hy2" -days 3650

# /etc/hysteria/config.yaml:
#   listen: 127.0.0.1:443
#   tls:
#     cert: /etc/hysteria/cert.pem
#     key: /etc/hysteria/key.pem
#   auth:
#     type: password
#     password: ваш-пароль

systemctl enable --now hysteria-server

# 2. Good TURN server
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:443

# 3. Получить строку конфигурации
GT_PASS=ваш-пароль ./server -generate-config -addr <IP-VPS>:56000
```
</details>

## Настройка клиента

### Вариант A: GUI-приложение (macOS / Windows)

1. Скачайте `.zip` из [Releases](https://github.com/politologhse/good-turn/releases)
2. Распакуйте и запустите **Good TURN**
3. Нажмите **Import config** — вставьте `gt://...` строку с сервера
4. В поле **VK link** вставьте ссылку на звонок
5. Нажмите кнопку подключения
6. Готово — настройте в браузере SOCKS5-прокси `127.0.0.1:1080` или HTTP `127.0.0.1:8080`

### Вариант B: CLI (Linux / macOS / Windows / Android)

**Терминал 1** — запустить TURN-туннель:

```bash
./client -peer <IP-VPS>:56000 -vk-link https://vk.com/call/join/...
```

**Терминал 2** — запустить Hysteria2-клиент:

```bash
hysteria client -c hysteria-client.yaml
```

<details>
<summary>hysteria-client.yaml</summary>

```yaml
server: 127.0.0.1:9000
auth: ваш-пароль
tls:
  sni: hy2
  insecure: true
socks5:
  listen: 127.0.0.1:1080
http:
  listen: 127.0.0.1:8080
```
</details>

После запуска — SOCKS5 на `127.0.0.1:1080`, HTTP на `127.0.0.1:8080`.

#### Маршруты (только CLI)

Чтобы трафик к TURN-серверам шёл напрямую, а не через прокси:

| ОС | Команда |
|----|---------|
| Linux | `./client ... \| sudo ./routes.sh` |
| macOS | `./client ... \| sudo ./routes-macos.sh` |
| Windows | `./client.exe ... \| ./routes.ps1` (PowerShell от админа) |
| Android | Termux: `termux-wake-lock`, затем запуск как на Linux |

## Скорость

VK ограничивает ~5 Мбит/с на одно TURN-подключение. Чтобы увеличить скорость:

```bash
./client -n 4 -peer ...
```

Это откроет 4 параллельных TURN-соединения (~20 Мбит/с), но может увеличить джиттер.

## Если не работает

| Симптом | Что делать |
|---------|-----------|
| Зависает на «Connecting» | VK-ссылка истекла — создайте новый звонок |
| `BOT` в логах | VK PoW-капча отклоняется (~50%). Подождите, следующая попытка обычно проходит |
| `ERROR_LIMIT` в логах | Слишком много попыток — подождите 5-10 минут или смените IP (VPN) |
| Подключается, но трафик не идёт | Проверьте что Hysteria2 запущен и пароль совпадает |
| Медленно | Попробуйте `-n 4` или `-udp` |
| TCP не работает | Добавьте флаг `-udp` |
| `address already in use` | Порт 1080 занят. Смените в Settings на другой (например 1081) |
| SOCKS5 подключается, но сайты не открываются | Настройте браузер на SOCKS5 прокси или используйте `curl --socks5-hostname` |
| IPv6 ошибки | VPS без IPv6. Отключите: `sysctl -w net.ipv6.conf.all.disable_ipv6=1` |

## Флаги CLI

<details>
<summary>Сервер</summary>

| Флаг | Описание |
|------|----------|
| `-listen` | Адрес (по умолчанию `0.0.0.0:56000`) |
| `-connect` | Адрес Hysteria2 (например `127.0.0.1:443`) |
| `-cert` / `-key` | DTLS-сертификат (по умолчанию генерируется автоматически) |
| `-generate-config` | Вывести `gt://` строку и выйти |
| `-addr` | IP:port VPS для config string |
| `-pass` / `GT_PASS` | Пароль для config string (env var безопаснее) |
| `-sni` | SNI для config string (по умолчанию `hy2`) |
</details>

<details>
<summary>Клиент</summary>

| Флаг | Описание |
|------|----------|
| `-peer` | Адрес VPS (`host:port`) |
| `-vk-link` | Ссылка на VK-звонок |
| `-listen` | Локальный UDP relay (по умолчанию `127.0.0.1:9000`) |
| `-udp` | Подключаться к TURN по UDP вместо TCP |
| `-n` | Параллельные TURN-подключения (по умолчанию 1) |
| `-turn` | Указать адрес TURN-сервера вручную |
| `-no-dtls` | Отключить DTLS-обфускацию (не рекомендуется) |
</details>

## Сборка из исходников

```bash
go build ./server
go build ./client
go test ./...

# GUI (нужен Wails CLI: go install github.com/wailsapp/wails/v2/cmd/wails@latest)
cd gui && wails build
```

## Благодарности

- [Turnel](https://github.com/KillTheCensorship/Turnel) — TURN relay
- [Hysteria](https://github.com/apernet/hysteria) — QUIC-прокси
