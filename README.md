# Good TURN

Обход блокировок через TURN-серверы VK-звонков. Трафик выглядит как обычный VK-видеозвонок.

```
Телефон/ПК → VK TURN сервер (в белом списке ТСПУ) → ваш VPS → Интернет
```

Только для учебных целей!

## Как это работает

ТСПУ не блокирует трафик к TURN-серверам VK (иначе сломаются звонки). Good TURN оборачивает QUIC-пакеты Hysteria2 в DTLS и пересылает через VK TURN relay на ваш VPS, где они расшифровываются и передаются в Hysteria2 сервер.

```
Клиент:                                   VPS:
  Браузер                                   good-turn server :56000
    → SOCKS5 :1080                            ↓ DTLS расшифровка
    → Hysteria2 клиент                        → Hysteria2 :443
      → QUIC/UDP → 127.0.0.1:9000              → Интернет
        → good-turn client
          → DTLS → VK TURN ═══UDP═══→
```

## Быстрый старт

### 1. Сервер (VPS)

```bash
curl -fsSL https://raw.githubusercontent.com/politologhse/good-turn/main/setup-server.sh | bash -s -- -pass мой-пароль
```

Скрипт установит Hysteria2 + Good TURN, создаст systemd-сервисы и выведет config string:

```
gt://eyJhIjoiMTg1LjEuMi4zOjU2MDAwIiwicCI6Im15cGFzcyIsInMiOiJoeTIifQ==
```

Или вручную:
```bash
GT_PASS=мой-пароль ./server -generate-config -addr 185.1.2.3:56000
```

### 2. Клиент (GUI)

1. Скачайте из [Releases](https://github.com/politologhse/good-turn/releases) для macOS или Windows
2. Откройте, нажмите **Import config**, вставьте `gt://...` строку
3. Вставьте VK-ссылку (создайте звонок в VK)
4. Нажмите кнопку подключения
5. SOCKS5 `127.0.0.1:1080`, HTTP `127.0.0.1:8080`

### 3. Клиент (CLI)

```bash
# Терминал 1: TURN relay
./client -peer 185.1.2.3:56000 -vk-link https://vk.com/call/join/... -listen 127.0.0.1:9000

# Терминал 2: Hysteria2
hysteria client -c hysteria-client.yaml
```

<details>
<summary>hysteria-client.yaml</summary>

```yaml
server: 127.0.0.1:9000
auth: мой-пароль
tls:
  sni: hy2
  insecure: true
socks5:
  listen: 127.0.0.1:1080
http:
  listen: 127.0.0.1:8080
```
</details>

## Что нужно

- **VK-ссылка** -- создайте звонок в VK (нужен аккаунт). Не нажимайте "завершить для всех". Ссылка вечная.
- **VPS за границей** -- любой Linux VPS.

## Установка сервера вручную

<details>
<summary>Без curl | bash</summary>

```bash
# Hysteria2
bash <(curl -fsSL https://get.hy2.sh/)
openssl req -x509 -nodes -newkey ec:<(openssl ecparam -name prime256v1) \
  -keyout /etc/hysteria/key.pem -out /etc/hysteria/cert.pem \
  -subj "/CN=hy2" -days 3650
# Создайте /etc/hysteria/config.yaml (listen: 127.0.0.1:443, password, tls)
systemctl enable --now hysteria-server

# Good TURN
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:443
```
</details>

## Параметры

### Сервер

| Флаг | По умолчанию | Описание |
|------|-------------|----------|
| `-listen` | `0.0.0.0:56000` | Адрес прослушивания |
| `-connect` | -- | Адрес Hysteria2 (`127.0.0.1:443`) |
| `-cert` / `-key` | -- | DTLS сертификат (авто-генерация если не указан) |
| `-generate-config` | -- | Сгенерировать `gt://` строку и выйти |
| `-addr` | -- | IP:port для config string |
| `-pass` / `GT_PASS` | -- | Пароль (env var предпочтительнее) |
| `-sni` | `hy2` | SNI для config string |

### Клиент

| Флаг | По умолчанию | Описание |
|------|-------------|----------|
| `-peer` | -- | Адрес VPS (host:port) |
| `-vk-link` | -- | VK-ссылка |
| `-listen` | `127.0.0.1:9000` | Локальный UDP relay |
| `-udp` | TCP | Подключаться к TURN по UDP |
| `-n` | `1` | Параллельные TURN-подключения |
| `-turn` | auto | Ручной адрес TURN-сервера |
| `-no-dtls` | -- | Без обфускации |

VK ограничивает ~5 Мбит/с на TURN-подключение. `-n 4` даст до ~20 Мбит/с, но увеличит джиттер.

## Платформы

| Платформа | Маршруты |
|-----------|----------|
| Linux | `./client ... \| sudo ./routes.sh` |
| macOS | `./client ... \| sudo ./routes-macos.sh` |
| Windows | `./client.exe ... \| ./routes.ps1` (PowerShell от админа) |
| Android | Termux, `termux-wake-lock` перед запуском |

## Сборка

```bash
# CLI
go build ./server
go build ./client

# GUI (нужен Wails CLI)
cd gui && wails build

# Тесты
go test ./...
```

## Благодарности

- [Turnel](https://github.com/KillTheCensorship/Turnel) -- часть кода TURN relay
- [Hysteria](https://github.com/apernet/hysteria) -- QUIC-прокси
