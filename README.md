# Good TURN

Прокси для обхода блокировок через TURN-серверы VK-звонков.

ТСПУ не трогает трафик к VK TURN (иначе сломаются звонки). Good TURN маскирует интернет-трафик под VK-видеозвонок и пробрасывает его через ваш VPS за рубежом. Российские сайты (VK, Яндекс, Ozon и т.д.) ходят напрямую, без туннеля.

> Только для учебных целей.

## Как работает

```
Ваш ПК                          VK TURN сервер              Ваш VPS
┌──────────┐    DTLS/STUN        (белый список)     UDP      ┌──────────┐
│ Браузер  ├──► Hysteria2 ──► good-turn client ═══════════► good-turn  │
│          │    SOCKS5          127.0.0.1:9000                server    ├──► Интернет
└──────────┘                                                 :56000    │
                                                             ↓         │
                                                             Hysteria2 │
                                                             :443      │
                                                             └──────────┘
Трафик на .ru домены и RU IP → напрямую (не через туннель)
```

## Что нужно

1. **VPS за границей** — любой Linux VPS
2. **VK-ссылка** — откройте [vk.com/calls](https://vk.com/calls), создайте звонок, скопируйте ссылку. Не нажимайте «завершить для всех».

## Установка

### Сервер (VPS) — одна команда

```bash
curl -fsSL https://raw.githubusercontent.com/politologhse/good-turn/main/setup-server.sh | bash -s -- -pass ваш-пароль
```

Выведет строку конфигурации `gt://...` — сохраните, она нужна для клиента.

<details>
<summary>Ручная установка</summary>

```bash
# Hysteria2
bash <(curl -fsSL https://get.hy2.sh/)
openssl req -x509 -nodes -newkey ec:<(openssl ecparam -name prime256v1) \
  -keyout /etc/hysteria/key.pem -out /etc/hysteria/cert.pem \
  -subj "/CN=hy2" -days 3650 \
  -addext "subjectAltName=DNS:hy2" -addext "keyUsage=digitalSignature,keyEncipherment" \
  -addext "extendedKeyUsage=serverAuth"
chown root:hysteria /etc/hysteria/key.pem && chmod 640 /etc/hysteria/key.pem

# Конфиг: /etc/hysteria/config.yaml
# listen: 127.0.0.1:443, tls cert/key, auth password
systemctl enable --now hysteria-server

# Good TURN server
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:443

# Строка конфигурации
GT_PASS=ваш-пароль ./server -generate-config -addr <IP>:56000
```
</details>

### Клиент — GUI (macOS / Windows)

1. Скачайте из [Releases](https://github.com/politologhse/good-turn/releases)
2. macOS: `xattr -cr good-turn.app` (снять карантин, приложение не подписано)
3. Откройте, **Import config** → вставьте `gt://...`
4. Вставьте VK-ссылку
5. Нажмите кнопку подключения
6. Системный прокси включается автоматически. Браузер сразу работает через туннель.

Российские сайты ходят напрямую — split tunneling встроен.

<details>
<summary>Клиент — CLI</summary>

```bash
# Терминал 1
./client -peer <IP>:56000 -vk-link https://vk.com/call/join/...

# Терминал 2
hysteria client -c hysteria-client.yaml
```

hysteria-client.yaml:
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
acl:
  inline:
    - direct(geoip:private)
    - direct(geosite:category-ru)
    - direct(geoip:ru)
    - proxy(all)
```

Маршруты (чтобы TURN-трафик не зацикливался):

| ОС | Команда |
|----|---------|
| Linux | `./client ... \| sudo ./routes.sh` |
| macOS | `./client ... \| sudo ./routes-macos.sh` |
| Windows | `./client.exe ... \| ./routes.ps1` |
| Android | Termux: `termux-wake-lock` + запуск как на Linux |
</details>

## Скорость

VK ограничивает ~5 Мбит/с на TURN-подключение. `-n 4` даст ~20 Мбит/с.

## Если не работает

| Симптом | Что делать |
|---------|-----------|
| Зависает на «Connecting» | VK-ссылка истекла — создайте новый звонок |
| `BOT` в логах | VK PoW-капча отклонена. Обычно проходит со 2-3 попытки автоматически |
| `ERROR_LIMIT` в логах | Подождите 5-10 минут, VK rate limit сбросится |
| `address already in use` | Порт занят — смените в Settings (например 1081) |
| IPv6 ошибки | На VPS: `sysctl -w net.ipv6.conf.all.disable_ipv6=1` |
| Медленно | `-n 4` или `-udp` |

<details>
<summary>Флаги CLI</summary>

**Сервер:**
`-listen` (0.0.0.0:56000), `-connect` (127.0.0.1:443), `-cert`/`-key`, `-generate-config`, `-addr`, `-pass`/`GT_PASS`, `-sni`

**Клиент:**
`-peer`, `-vk-link`, `-listen` (127.0.0.1:9000), `-udp`, `-n`, `-turn`, `-no-dtls`
</details>

## Сборка

```bash
go build ./server && go build ./client && go test ./...
# GUI: go install github.com/wailsapp/wails/v2/cmd/wails@latest && cd gui && wails build
```

## Благодарности

- [Turnel](https://github.com/KillTheCensorship/Turnel) — TURN relay
- [Hysteria](https://github.com/apernet/hysteria) — QUIC-прокси
