# Monitoring Service

Сервис отслеживает значения в JSON и HTML по заданным правилам и отправляет уведомления при выполнении условий. Через HTTP API пользователи управляют мониторами, правилами и контактами; фоновые обработчики выполняют проверки по расписанию.
Получение JSON/HTML выполняет отдельный [Fetcher stack](https://github.com/wasss4bi/values-from-body-fetcher) через mTLS gRPC. В Monitoring-Service остаются пользователи, роли, расписание, сравнения, retry и доставка уведомлений.

## Запуск

1. Настройте локальный `.env` по `.env.example`. Обязательны JWT secret, ключ шифрования сессий, `ADMIN_EMAIL`, `ADMIN_PASSWORD` и `BROWSERLESS_TOKEN`.
2. Авторизуйте Docker для приватного GHCR: `docker login ghcr.io -u wasss4bi` с правом `read:packages`. Токен не хранится в Git.
3. Подготовьте сертификаты и проверьте версии по [инструкции Fetcher](docs/fetcher/README.md). Контракт импортируется как приватный Go module `github.com/wasss4bi/values-from-body-fetcher@v0.1.1`; для его скачивания нужен GitHub-доступ к репозиторию, отдельно от GHCR pull.
4. Соберите приложение и примените миграции отдельной командой:

```powershell
./tools/build-go.ps1
docker compose up -d postgres
docker compose run --rm --no-deps monitoring_service /usr/local/bin/migration
docker compose pull fetcher parser playwright browserless squid
docker compose up -d
```

API: `http://localhost:8080`; Swagger: `/swagger`; контракт: `/openapi.json`.
[Bruno workspace](docs/bruno/workspace.yml) содержит отдельный вход администратора.
Метрики API доступны внутри Docker на `monitoring_service:9100/metrics`.

Миграции не выполняются автоматически при старте API. Миграция 2 однократно создаёт администратора из ENV; повторный запуск не меняет пароль. Новая серия миграций предназначена для пустой БД: подробности в [описании схемы](docs/migration-baseline.md).

## Доступ

Регистрация `POST /users` и `POST /auth/login` публичны. Роль хранится в PostgreSQL и проверяется на каждом защищённом запросе. Версионируемая политика Casbin задаёт:

- Все `/target-hostnames*`, список/удаление пользователей и чужие профили — admin.
- Чтение и изменение собственного профиля/пароля — владелец или admin.
- Чтение глобального каталога `/contacts` — авторизованный пользователь; изменения — admin.
- `/me/*` — данные владельца JWT.

HTTP API управления ролями отсутствует. Новые bootstrap-правила доступны только администратору через `/target-hostnames`.

## Мониторинг

`POST /me/monitors` и добавление правила требуют `Idempotency-Key`. Монитор содержит от одного до трёх неизменяемых правил. Списки мониторов и уведомлений используют cursor pagination.

Scheduler создаёт durable задания только для активных мониторов с доступным подтверждённым контактом. Workers получают scalar-результаты от Fetcher, сравнивают значения и атомарно фиксируют evaluation, состояние, session CAS, inbox и notification outbox. Изменение правил увеличивает generation и перепланирует активный монитор; устаревший результат отклоняется.

Разрешены только активные hostname из PostgreSQL и пользовательский GET. `page_url` обязателен как ссылка на товар; `browser_page_url` выбирает browser JSON capture, `json_source` — JSON внутри HTML. Пользовательские headers отключены. Сессии зашифрованы ключом Monitoring-Service. История пропущенных интервалов не воспроизводится: после восстановления выполняется актуальная проверка. Infrastructure outage откладывает задание без расходования target attempts.

[Архитектура](docs/architecture/README.md), [эксплуатация Fetcher](docs/fetcher/README.md), [примеры мониторинга](docs/bruno-price-monitors.md).
