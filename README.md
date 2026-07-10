# Monitoring Service

Сервис отслеживает значения в JSON и HTML по заданным правилам и отправляет уведомления при выполнении условий. Через HTTP API пользователи управляют мониторами, правилами и контактами; фоновые обработчики выполняют проверки по расписанию.
Получение JSON/HTML выполняет отдельный [Fetcher stack](https://github.com/wasss4bi/values-from-body-fetcher) через mTLS gRPC. В Monitoring-Service остаются пользователи, роли, расписание, сравнения, retry и доставка уведомлений.

## Запуск

1. Настройте локальный `.env` по `.env.example`. Обязательны JWT secret, ключ шифрования сессий, `ADMIN_EMAIL`, `ADMIN_PASSWORD` и `BROWSERLESS_TOKEN`.
2. Авторизуйте Docker для приватного GHCR: `docker login ghcr.io -u wasss4bi` с правом `read:packages`. Токен не хранится в Git.
3. Подготовьте сертификаты и проверьте версии по [инструкции Fetcher](docs/fetcher/README.md). Контракт импортируется как приватный Go module `github.com/wasss4bi/values-from-body-fetcher@v0.6.0`; для его скачивания нужен GitHub-доступ к репозиторию, отдельно от GHCR pull.
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

Миграции не выполняются автоматически при старте API. Миграция 2 однократно создаёт администратора из ENV; повторный запуск не меняет пароль. Порядок применения описан в [описании схемы](docs/migration-baseline.md).

Серверный Compose, подготовка секретов GitHub Actions и порядок автоматического развёртывания описаны в [инструкции по развёртыванию](docs/deployment.md).

## Короткий демосценарий

Нужен собственный доступный по HTTPS тестовый endpoint, возвращающий, например, `{"price": 100}`, и настроенная отправка Telegram или email. Замените `demo.example.com` ниже своим разрешённым hostname; его адрес должен быть доступен Fetcher и проходить его сетевые проверки.

1. В [Bruno](docs/bruno/workspace.yml) выполните вход администратора и зарегистрируйте `demo.example.com` через `POST /target-hostnames` с включённым `is_active` и подходящим лимитом запросов.
2. Через `/contacts` настройте канал, затем через `/me/contacts` создайте контакт, запросите и подтвердите код и отдельно включите контакт. Для Telegram предварительно откройте диалог с ботом.
3. Создайте монитор через `POST /me/monitors`, указав уникальный заголовок `Idempotency-Key`:

   ```json
   {
     "name": "Демо: цена больше нуля",
     "url": "https://demo.example.com/product.json",
     "page_url": "https://demo.example.com/product.json",
     "type": "json",
     "path_type": "json",
     "rules_count_to_finish": 1,
     "rules": [{"path": "price", "operator": ">", "value": 0, "description": "Цена больше нуля"}]
   }
   ```

4. Проверьте `GET /me/monitors/{id}` и `GET /me/notifications`: при успешном чтении положительной цены монитор завершится, а notification worker отправит сообщение. `sent` означает, что внешний сервис принял сообщение; прочтение получателем не проверяется.

В API и notification worker обязателен `JWT_SECRET`. Значения duration, если заданы, должны быть положительными и иметь формат Go, например `30s` или `10m`; некорректное значение останавливает запуск процесса.

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
