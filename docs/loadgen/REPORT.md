# Отчёт: нагрузочная база PostgreSQL

Примечание к текущей версии: поддерживается только large. Упоминания прежнего small ниже относятся к истории проверок; исходные отчёты сохранены как доказательства выполненных тестов.

Подготовлена отдельная БД `monitoring_service_loadtest` в контейнере `monitoring_service-loadgen-postgres-1`, volume `monitoring_service-loadgen_data`. Рабочая БД приложения не использовалась. Дата проверки: 4 ноября 2026 года (Asia/Omsk).

## Фактический объём

| Таблица | Строк | COPY, секунд |
|---|---:|---:|
| users | 100 001 | 1,42 |
| contacts | 2 | 0,00 |
| target_hostnames | 1 000 | 0,03 |
| user_contacts | 219 958 | 25,36 |
| monitored_endpoints | 1 000 000 | 99,04 |
| monitors | 2 000 000 | 356,65 |
| monitor_rules | 3 000 000 | 364,58 |
| endpoint_fetches | 2 500 000 | 624,91 |
| monitor_fetch_evaluations | 2 000 000 | 348,38 |
| notifications | 1 000 000 | 205,80 |
| outbox_events | 3 500 000 | 157,02 |
| processed_messages | 1 000 000 | 27,20 |
| idempotency_keys | 1 000 000 | 97,66 |

Всего: **17 320 961 строк**. Users включает сохранённого migration seed user.

Сумма фактических COPY-таймеров из лога: **00:38:28**. Финализация после перезапуска тестового PostgreSQL (ANALYZE, проверки, контрольные суммы): **00:04:38**. Это отдельные измеренные этапы, а не время единого непрерывного CLI-запуска. Размер БД: **4,79 GiB** (5 147 327 511 байт). Это размер таблиц, индексов и служебных данных базы; WAL всего кластера в него не входит.

Параметры: seed=42, batch_size=10000, as_of=2026-11-04T02:42:51Z, date_from=2026-08-06T02:42:51Z. Числа — результат этого запуска, не прогноз для другой машины.

Первичная проверка после успешной загрузки остановилась из-за Docker /dev/shm=64 MiB. Тестовый контейнер пересоздан с /dev/shm=1 GiB с сохранением volume; ограничения PostgreSQL и planner settings не отключались. Проверки завершены отдельно на сохранённых строках теми же функциями ANALYZE/check/fingerprints/distributions, которые вызывает финальный CLI. В ходе большого прогона также уточнялись fencing остановленных мониторов, retry/временные поля и перемешивание terminal-событий; уже записанные синтетические строки согласованы до manifest. Физический размер включает эти UPDATE. Это не неизменённый повтор финального CLI с нуля. Дополнительно выполнено read-only сравнение samples с финальным генератором. Точная воспроизводимость финального генератора подтверждена двумя чистыми small-загрузками с батчами 137 и 777; Docker --validate-only также проверен на small.

## Проверки

* 69 проверок: FK, UNIQUE/PK, NOT NULL, включённые constraints/triggers, владельцы fetch/evaluation/notification, URL, поколения, правила, даты, Outbox payload и Inbox — нарушений нет.
* Окончательная small-модель: одинаковые контрольные суммы всех таблиц при batch_size=137 и 777; seed и даты одинаковые.
* Reset отказался удалять добавленного контрольного пользователя; запись сохранилась. Production и reset без точного подтверждения отклонены до подключения к БД.
* `go test ./cmd/... ./internal/...` и `go vet ./cmd/loadgen ./tools/loadgen` прошли. Генератор дополнительно проверен на PostgreSQL 16 с текущими миграциями, без RabbitMQ/Redis/HTTP/SMTP/Telegram.
* `schema_migrations` имеет version=21, dirty=false; генератор не записывает эту таблицу. Application indexes и SQL исходников не изменены.

Кандидаты на момент as_of:

| Выборка | Строк |
|---|---:|
| Scheduler | 312 849 |
| Fetch worker | 141 150 |
| Outbox publisher | 60 587 |
| Notification worker | 169 473 |

Полные статусы, сроки, counts и контрольные суммы: [large-verified-report.json](reports/large-verified-report.json). Они относятся к сохранённому as_of. Оригинальные запросы используют настоящий NOW(), поэтому просроченность при следующих запусках меняется.

## Исходные планы

17 запросов извлечены из Go исходников без изменения SQL. Все проверены на small и large; DML измерен с ROLLBACK. Оптимизация запросов и добавление application indexes не выполнялись.

| SQL-файл | Planning, ms | Execution, ms |
|---|---:|---:|
| [01-scheduler](reports/plans/01-scheduler.txt) | 36,358 | 5 536,010 |
| [02-fetch-owner-lock](reports/plans/02-fetch-owner-lock.txt) | 1,075 | 0,880 |
| [03-fetch-claim](reports/plans/03-fetch-claim.txt) | 3,002 | 8,810 |
| [04-evaluation-monitor-rules](reports/plans/04-evaluation-monitor-rules.txt) | 1,196 | 0,260 |
| [05-rules-batch](reports/plans/05-rules-batch.txt) | 0,869 | 0,696 |
| [06-outbox-claim](reports/plans/06-outbox-claim.txt) | 2,220 | 1,047 |
| [07-notification-claim](reports/plans/07-notification-claim.txt) | 3,098 | 5,472 |
| [08-notification-recovery](reports/plans/08-notification-recovery.txt) | 5,486 | 666,469 |
| [09-fetch-history-get](reports/plans/09-fetch-history-get.txt) | 0,656 | 0,710 |
| [10-inbox-exists](reports/plans/10-inbox-exists.txt) | 0,315 | 0,207 |
| [11-inbox-record-once](reports/plans/11-inbox-record-once.txt) | 0,211 | 0,299 |
| [12-api-idempotency](reports/plans/12-api-idempotency.txt) | 2,444 | 41,559 |
| [13-fetch-recovery](reports/plans/13-fetch-recovery.txt) | 10,360 | 76,628 |
| [14-endpoint-active-monitors](reports/plans/14-endpoint-active-monitors.txt) | 0,369 | 0,576 |
| [15-fetch-retention](reports/plans/15-fetch-retention.txt) | 9,309 | 839,286 |
| [16-api-monitor-list](reports/plans/16-api-monitor-list.txt) | 7,981 | 0,720 |
| [17-api-notification-list](reports/plans/17-api-notification-list.txt) | 2,318 | 53,715 |

Замеры — один последовательный запуск каждого запроса. Cache не сбрасывался; гарантированный cold cache не заявляется. EXPLAIN с ROLLBACK сохраняет логические строки, но может менять страницы/WAL/cache. Параметры PostgreSQL сохранены в [postgres-settings.json](reports/postgres-settings.json).

В текущем коде нет endpoint-history SELECT с пагинацией: файл 09 измеряет сохранённый repository Get по fetch UUID. Метод ActiveMonitors фактически принимает monitor ID; файл 14 содержит repository-выборку мониторов endpoint из SwitchEndpointType. Текущие runtime-вызовы методов 09/14 не найдены: это дополнительные repository-only планы, не актуальные горячие пути. Никакие придуманные аналоги не подменяют application queries.

## Использование готового снимка

Из корня `D:\ITSHNICHNAYA\Projects\Go\Monitoring-Service`:

```powershell
docker compose -f tools/loadgen/compose.yaml exec -T postgres psql -X -U loadgen -d monitoring_service_loadtest
```

Для SQL через локальный сокет контейнера пароль не требуется. Для повторной генерации нужен пароль уже существующего test volume: локальный файл `C:\Users\lbvf0\Documents\Codex\2026-11-03\files-pasted-by-the-user-postgresql\work\test.env`. Его значение не включено в архив и Git. Для новой отдельной установки задайте собственный LOADGEN_PASSWORD по [инструкции](README.md).

Дополнительная БД `monitoring_service_smoke_loadtest` хранит small-снимок для проверки. Архив содержит код, профили, SQL и отчёты; многогигабайтный dump в него не включён. Подготовленные базы остаются в отдельном Docker volume.

Изменения проекта: только новые каталоги `cmd/loadgen`, `docs/loadgen`, `tools/loadgen`. Коммиты и push не выполнялись.


