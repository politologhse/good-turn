# Good TURN

Обход блокировок через TURN-серверы VK-звонков. Трафик выглядит как обычный VK-видеозвонок.

```
Телефон/ПК → VK TURN сервер (в белом списке ТСПУ) → ваш VPS → Интернет
```

Только для учебных целей!

## Быстрый старт

### 1. Сервер (VPS) — одна команда

```bash
curl -fsSL raw.githubusercontent.com/politologhse/good-turn/main/setup-server.sh | bash -s -- -pass мой-пароль
```

Скрипт установит Hysteria2 + Good TURN, создаст systemd-сервисы и выведет **config string**:

```
gt://eyJhIjoiMTg1LjEuMi4zOjU2MDAwIiwicCI6Im15cGFzcyIsInMiOiJoeTIifQ==
```

Сохраните эту строку — она нужна для клиента.

Или вручную:
```bash
./server -generate-config -addr 185.1.2.3:56000 -pass мой-пароль -sni hy2
```

### 2. Клиент (десктоп) — GUI приложение

1. Скачайте приложение + положите рядом бинарник `hysteria`
2. Откройте приложение
3. **Import config** → вставьте `gt://...` строку от админа
4. Вставьте VK-ссылку (создайте звонок в VK)
5. Нажмите кнопку ⏻
6. Готово. SOCKS5 на `127.0.0.1:1080`, HTTP на `127.0.0.1:8080`

### 3. Клиент (CLI)

```bash
# Запустить TURN relay
./client -peer 185.1.2.3:56000 -vk-link https://vk.com/call/join/... -listen 127.0.0.1:9000

# Запустить Hysteria2 клиент (в другом терминале)
hysteria client -c hysteria-client.yaml
```

`hysteria-client.yaml`:
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

---

## Что нужно

1. **VK-ссылка** — создайте звонок в VK (нужен аккаунт). Не нажимайте "завершить для всех". Ссылка вечная.
2. **VPS за границей** — любой, куда можно поставить Hysteria2

## Установка сервера вручную

Если не хотите `curl | bash`:

```bash
# 1. Hysteria2
bash <(curl -fsSL https://get.hy2.sh/)

# Самоподписанный сертификат
openssl req -x509 -nodes -newkey ec:<(openssl ecparam -name prime256v1) \
  -keyout /etc/hysteria/key.pem -out /etc/hysteria/cert.pem \
  -subj "/CN=hy2" -days 3650

# /etc/hysteria/config.yaml
# listen: 127.0.0.1:443
# tls: { cert: /etc/hysteria/cert.pem, key: /etc/hysteria/key.pem }
# auth: { type: password, password: мой-пароль }

systemctl enable --now hysteria-server

# 2. Good TURN server
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:443

# 3. Сгенерировать config string
./server -generate-config -addr <ваш-IP>:56000 -pass мой-пароль
```

#### Docker

```bash
docker run -p 56000:56000/udp -e CONNECT_ADDR=127.0.0.1:443 good-turn
```

## Платформы

| Платформа | Маршруты | Команда |
|-----------|----------|---------|
| Linux | `\| sudo ./routes.sh` | Добавить к команде client |
| macOS | `\| sudo ./routes-macos.sh` | Добавить к команде client |
| Windows | `\| ./routes.ps1` | PowerShell от админа |
| Android | Termux | `termux-wake-lock` перед запуском |

Скрипты маршрутов нужны чтобы трафик к TURN-серверам шёл напрямую.

## Параметры

### Сервер

| Флаг | По умолчанию | Описание |
|------|-------------|----------|
| `-listen` | `0.0.0.0:56000` | Адрес прослушивания |
| `-connect` | — | Адрес Hysteria2 (e.g. `127.0.0.1:443`) |
| `-generate-config` | — | Сгенерировать `gt://` строку и выйти |
| `-addr` | — | IP:port для config string |
| `-pass` | — | Пароль для config string |
| `-sni` | `hy2` | SNI для config string |

### Клиент

| Флаг | По умолчанию | Описание |
|------|-------------|----------|
| `-peer` | — | Адрес VPS (host:port) |
| `-vk-link` | — | VK-ссылка |
| `-listen` | `127.0.0.1:9000` | Локальный адрес |
| `-udp` | TCP | Подключаться к TURN по UDP |
| `-n` | `1` | Параллельные TURN-подключения |
| `-turn` | auto | Ручной адрес TURN-сервера |
| `-no-dtls` | — | Без DTLS обфускации |

### Скорость

VK ограничивает ~5 Мбит/с на TURN-подключение. `-n 4` даст до ~20 Мбит/с, но может увеличить джиттер.

## Благодарности

- https://github.com/KillTheCensorship/Turnel
- https://github.com/apernet/hysteria
