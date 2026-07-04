# Bruno

## Idempotency-Key

Общий before-request скрипт коллекции gO для POST, PUT, PATCH и DELETE
подставляет случайный UUID, если заголовок Idempotency-Key отсутствует
или содержит только пробелы. Явно указанный непустой заголовок сохраняется
без изменений, независимо от регистра имени. GET/HEAD/OPTIONS не меняются.

Переменная env idempotencyKey больше не обязательна. При желании её можно
явно использовать в заголовке как {{idempotencyKey}}, либо вписать ключ напрямую.

Каждая новая отправка без ручного ключа получает новое значение. Для повтора
той же операции после неизвестного результата скопируйте прежний ключ из
Timeline в Headers: без этого повтор будет считаться новым запросом.
Скрипт не сохраняет автоматически созданный ключ в env.

## Ошибки в запросах

Общий контракт описан в [api-errors.md](../api-errors.md).

- `error.code` определяет HTTP-статус, `error.message` объясняет причину.
- Для невалидных параметров смотрите также `error.fields`.
- `409` при создании монитора может означать несовпадение `type`,
  `browser_page_url` или `json_source` у уже зарегистрированного API URL.
  В сообщении указано, какое именно поле конфликтует.
- Для браузерного получения JSON передавайте `browser_page_url` явно:
  ссылка `page_url` сама по себе не выбирает этот способ получения данных.
- Старый Idempotency-Key воспроизводит сохранённый ответ. Изменение запроса
  требует нового ключа; при неизвестном результате прежнего создания сначала
  проверьте его и повторяйте исходный запрос с прежним ключом.
- Неожиданный серверный сбой содержит безопасное описание операции;
  технические подробности не должны появляться в клиентском ответе.

## Роли и вход администратора

Задайте `adminEmail` и `adminPassword` в локальном игнорируемом Bruno environment из локального `.env` приложения. Запрос `Authentication / Login admin` сохраняет `adminAccessToken`. Обычный Login сохраняет `accessToken`; Create user сохраняет `userId` для запросов собственного профиля. Секреты и токены не добавляйте в workspace.yml.

Запросы target-hostnames, изменения глобальных contacts и административные Users используют `adminAccessToken`. Негативная проверка bootstrap policy использует обычный `accessToken` и ожидает 403. После SQL-отзыва роли прежний JWT теряет административный доступ сразу.

An idempotency key is bound to the user, HTTP method, request URI and body. Reusing it for a different operation or resource returns HTTP 409. Keys created before this request fingerprint change cannot be replayed until they expire; use a new key for new requests.
