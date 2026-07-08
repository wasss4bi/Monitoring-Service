# Fetcher в Monitoring-Service

Авторитетный контракт и реализация находятся в приватном [values-from-body-fetcher](https://github.com/wasss4bi/values-from-body-fetcher).
Monitoring-Service импортирует `github.com/wasss4bi/values-from-body-fetcher/api/fetcher/v1` из версии `v0.5.0`, закреплённой в `go.mod` и `go.sum`. Локальных generated stubs и proto source нет. Здесь остаются клиентский mTLS transport и бизнес-адаптер результатов.

## Доступ к приватному Go module

Для локальных Go-команд настройте Git-доступ к репозиторию и исключите private module из public proxy/checksum service:

```powershell
$env:GOPRIVATE = 'github.com/wasss4bi/values-from-body-fetcher'
go mod download
go test -mod=readonly ./...
```

Если GOPRIVATE уже содержит другие private modules, добавьте путь к существующему списку через запятую. Настройка не создаёт локальный replace: Go скачивает выбранный Git tag, а go.sum фиксирует содержимое. GitHub нужен на этапе получения зависимостей; runtime использует FETCHER_ADDRESS по mTLS gRPC.

Для Docker используйте `./tools/build-go.ps1` (или `-Development`). Скрипт берёт существующий GitHub credential из Git Credential Manager либо временно использует уже заданный `GO_MODULE_GITHUB_TOKEN`, передаёт его через BuildKit secret и восстанавливает environment после успеха/ошибки. Токен не записывается в Dockerfile ARG/ENV или файл проекта. Нужен доступ чтения репозитория; один лишь GHCR read:packages его не заменяет. При прямом `docker compose build monitoring_service`/`make dev` этот build-only token задаётся в environment вызывающего процесса.

Dev image заранее скачивает private module при сборке. Старый go_module_cache volume сохранён, но больше не закрывает встроенный module cache image: runtime не получает GitHub token. После изменения go.mod/go.sum dev image нужно пересобрать. `-Development` собирает image; запуск: `docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d monitoring_service`.

Документация: [Go private modules](https://go.dev/ref/mod#private-modules), [Docker build secrets](https://docs.docker.com/build/building/secrets/).

## Установка и обновление

Все пять Linux/amd64 образов в основном Compose закреплены выбранной версией и SHA256 digest. `docs/fetcher/images.json` фиксирует manifest выпуска. Изменение тега `dev` не обновляет Monitoring-Service.

1. Выполните `docker login ghcr.io -u wasss4bi` с PAT classic `read:packages`; Docker хранит credential отдельно от Git.
2. Получите development сертификаты командой из checkout Fetcher:

   ```sh
   go run ./tools/fetcher/certgen.go -out D:/ITSHNICHNAYA/Projects/Go/Monitoring-Service/tmp/fetcher-certs
   ```

   Генератор не перезаписывает существующие файлы и не сохраняет CA private key. Серверный SAN — `fetcher`, клиентский — `monitoring`, разные EKU, TLS 1.3. Development leaf действуют 7 дней; для постоянной среды используйте собственную PKI и контролируемую замену сертификатов. Server key должен читаться UID 10001; client key остаётся только у Monitoring.
3. Настройте `.env` по `.env.example`, включая случайный `BROWSERLESS_TOKEN`, ADMIN credentials и ключ шифрования сессий. Сохраните прежний ключ шифрования для provided-сессий.
4. До перехода остановите старых fetch consumers, scheduler и единственного владельца профилей; дождитесь окончания inflight или lease. Не запускайте старые и новые workers на одной очереди во время смены версии.
5. Соберите `monitoring_service` через `./tools/build-go.ps1` (для PowerShell), поднимите PostgreSQL и выполните `/usr/local/bin/migration` отдельным `docker compose run --rm --no-deps monitoring_service ...`. Проверьте baseline и dirty state; не используйте force.
6. Выполните `docker compose pull fetcher parser playwright browserless squid`, затем `docker compose up -d`.

Текущая последовательность миграций описана в [migration-baseline.md](../migration-baseline.md): схема создаётся миграцией 1, администратор и служебные записи — миграцией 2. Повторное применение не меняет пароль администратора.

Для следующего выпуска скачайте `images.json` из GitHub release, проверьте общий source commit/version и замените все пять image pins вместе. Версию Go module обновляйте явно через `go get github.com/wasss4bi/values-from-body-fetcher@vX.Y.Z`, затем `go mod tidy` и тесты. Контракт и образы должны соответствовать выбранному совместимому выпуску; локальный `replace`/копирование generated файлов не используются. Старые образы с несовместимыми migrations не запускайте. Возврат версии требует остановки consumers и владельца profiles; rollback не восстанавливает очищенные managed-сессии.

## Граница и безопасность

Monitoring владеет PostgreSQL, RabbitMQ, правилами, сравнениями, расписанием, target retry и уведомлениями. Fetcher получает только запрос извлечения, opaque profile scope и session envelope. PostgreSQL/RabbitMQ/Telegram credentials и ключ шифрования Monitoring-Service ему не передаются.

Пользовательские мониторы выполняют GET по HTTPS/443. Bootstrap exceptions хранятся в PostgreSQL на hostname и передаются только в PrepareSession: точный HTTPS origin, path prefix и GET/HEAD/POST/OPTIONS. Путь не является scope сессии. Все `/target-hostnames*` защищены Casbin admin-role; изменение bootstrap policy отклоняет старые наблюдения и перепланирует активные проверки. `reset-session` сбрасывает только managed-сессии выбранного hostname и сохраняет provided.

Workers и probe доступны к facade по mTLS в default network. Engines закрыты в `fetcher_engines`; браузерный egress проходит через Squid в internal `browser_egress`. Порты Node/Browserless/Squid и gRPC не публикуются на хост. Сертификат клиента не даёт произвольных HTTP methods/headers/body в extraction RPC.

Fetcher отклоняет mixed public/private DNS ответы, специальные сети и четвёртый или cross-origin redirect. Browser response storage/CacheStorage/service workers выключены; cookies/localStorage/IndexedDB сохраняются для анонимного профиля. Отсутствие доступа к реальному сайту, CAPTCHA или отказ upstream не обходятся этим стеком.

## Семантика значений и orchestration

Scalar cache имеет TTL 10 минут от извлечения. Ключ включает canonical URL и effective selector/source/mode; query отсортирован, trailing slash удалён кроме `/` в корне. ID пользователя/монитора/сессии в ключе нет. Региональные, валютные и AB-различия гостевых сессий могут смешиваться — это согласованный компромисс. Mixed-age результаты допускаются; это не атомарный snapshot сайта. Полные bodies не сохраняются в Redis или новых fetch rows. SQL очистки исторических bodies остаётся `tools/monitoring/clear-historical-bodies.sql` и не запускался на рабочей БД.

Неверный selector — terminal error; missing/timeout — невыполненное правило. Общая fetch failure подавляет trigger по partial cache hits. Числовое сравнение/number format остаются в Monitoring-Service; raw JSON numbers не проходят float64/JS Number в Fetcher.

| Параметр | Значение |
|---|---|
| Direct RPC / attempt | 5 s / 15 s |
| Browser/Prepare RPC / attempt | 100 s / 300 s |
| Completion reserve | 1 s direct / 5 s browser |
| Worker lease | минимум 330 s |
| RPC calls на одну attempt | максимум 3, включая Prepare/re-evaluation |
| Reconnect jitter | 0–100 ms, затем 0–200 ms |
| Fetcher admission | 4 direct, 1 Node operation, 8 ожидающих |
| Health probe | каждые 2 s, deadline 1 s; DOWN после 3 ошибок, UP после 2 успехов |

Infrastructure failure/capacity откладывают тот же fetch_id без target error counters. Новая delivery имеет новый message_id; inbox защищает прежнюю delivery. Target retry сохраняет 6 attempts/full jitter; Retry-After пока не учитывается. После outage выполняется одно актуальное наблюдение. Redis outage допускает cache miss и per-process local limiter; при N workers суммарная частота может достичь N×L, restart очищает локальную историю.

Финализация атомарно проверяет generation/token/lease, сохраняет evaluation, monitor state, session CAS, notifications/outbox/inbox. Изменение правил/threshold увеличивает generation, сбрасывает retry и перепланирует только активный монитор. Терминальный результат оставляет next_check_at=NULL.

Dependency direct/browser state и probe lease хранятся в PostgreSQL. DOWN/UP и admin alert создаются одной транзакцией; Telegram доставляется существующим notification worker с at-least-once. Реальные уведомления в тестах не отправлялись. Список получателей задаёт `FETCHER_ADMIN_TELEGRAM_CHAT_IDS`.

## Проверки


Fetcher CI проверяет proto regeneration, Go unit/race, Redis, Node unit, browser fixtures без внешней сети, настоящие HTTPS redirects и многократное сохранение профиля после закрытия Chrome. Публикация всех пяти образов разрешена только после проверок; CI проверяет private visibility и создаёт release manifest. PR ничего не публикует; main публикует dev/full SHA; semver tag создаёт release. Автоматического деплоя сервера нет.

Нагрузочные свойства/coverage/SLA не измерялись. Успешные fixtures не гарантируют доступность стороннего сайта. Ручная диагностика профилей и её скрипты перенесены в репозиторий Fetcher; его helper приостанавливает facade и использует отдельный диагностический volume, не активные Browserless profiles.

Миграции применяйте через Go runner (`/usr/local/bin/migration` или `go run ./cmd/migration`, `make migrate-up`). Обычный SQL CLI не вычисляет bcrypt bootstrap hash администратора; step/goto-команды не подходят для перехода через миграцию 2 без подготовки выделенного соединения.

Обновляйте Browserless вместе с подключённым HTML engine: при отдельном пересоздании provider его прежнее CDP-соединение требуется заменить перезапуском engine.

