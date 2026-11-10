# API

База: `/api`. JSON, поля в `camelCase`, время — **миллисекунды Unix**, деньги — целые рубли.
Сессия — httpOnly-cookie `session` (ставится при регистрации/входе, живёт 7 дней). Для не-браузерных
клиентов поддержан заголовок `Authorization: Bearer <токен>`.
Идентификатор пользователя **никогда** не передаётся в запросах — сервер берёт его из сессии.

Ошибки: `{ "error": "текст для пользователя", "code": "machine_code" }` + HTTP-статус
(400 валидация, 401 не вошли, 403 нет прав, 404, 409 конфликт состояния, 429 лимит входа, 413/415 загрузка).

## Таймеры
Не считайте оставшееся время по `Date.now()` клиента: запросите `GET /api/time` → `{ "now": ms }`,
запомните разницу с `Date.now()` и вносите её в расчёт. Статус лота сервер уже отдаёт с учётом `endsAt`.
Обратный отсчёт считается на клиенте до `endsAt`; `endsAt` может **вырасти** (Anti-snipe, см. ниже), поэтому
всегда берите актуальное значение из последнего ответа или WebSocket-события. Серверное время приходит и в
каждом WebSocket-сообщении (`now`), так что поправку можно уточнять без отдельных запросов.

## Справочники
| Метод | Путь | Ответ |
|---|---|---|
| GET | `/api/categories` | `[{ id, name, description, subs: [{ id, name }] }]` |
| GET | `/api/dorms` | `[{ id, name }]` |
| GET | `/api/time` | `{ now }` |

## Авторизация
| Метод | Путь | Тело | Ответ |
|---|---|---|---|
| POST | `/auth/register` | `{ name, username, telegram, dormId, password, confirm }` | 201 `User` + cookie |
| POST | `/auth/login` | `{ username, password }` | `User` + cookie (10 попыток / 5 мин с одного IP) |
| POST | `/auth/logout` | — | `{ status }` |
| GET | `/auth/me` | — | `User` или 401 |

Правила: никнейм 3–32 (буквы/цифры/`_.-`, без учёта регистра уникален); Telegram 5–32 (`@` можно),
уникален; пароль 6–72 байта; `dormId` — из `/dorms`.

`User = { id, username, name, telegram, dormId, createdAt }` — отдаётся только владельцу.
Чужие профили не публичны: имя автора лота — `authorUsername`, имена в ставках — `username`.

## Профиль (нужна сессия)
| Метод | Путь | Тело / ответ |
|---|---|---|
| PUT | `/me/profile` | `{ name, telegram, dormId }` → `User` |
| PUT | `/me/username` | `{ newUsername }` → `User` |
| PUT | `/me/password` | `{ current, newPassword, confirm }` → `{ status }`; текущий пароль проверяется |
| GET | `/me/stats` | `{ created, participated, won, sold, rating, complaints }` (`rating = won + sold`, жалоб пока нет → 0) |
| GET | `/me/lots` | `[Lot]` — мои лоты (все статусы) |
| GET | `/me/bids` | `[Lot]` — лоты, где я ставил; `myMaxBid` — моя максимальная ставка |

## Лоты
`Lot = { id, title, description, image, category, subcategory, authorId, authorUsername, dormId, startPrice,
step, currentPrice, minBid, bidsCount, leaderId|null, myMaxBid, createdAt, endsAt, status, contact? }`

* `status`: `active | finished | cancelled` (уже с учётом `endsAt`).
* `currentPrice` — лидирующая ставка или `startPrice`; `minBid` — `startPrice`, пока ставок нет (первая ставка может равняться стартовой цене), далее `currentPrice + step`.
* `dormId` — общежитие продавца. `myMaxBid` = 0 для анонима или если ставок нет.
* `contact = { role, name, telegram }` — **только** продавцу и победителю завершённого лота со ставками
  (`role`: «Победитель» / «Продавец»). Остальным поля нет.

| Метод | Путь | Описание |
|---|---|---|
| GET | `/lots` | Каталог. Query: `category`, `sub`, `q`, `status` (`active` по умолчанию, `finished`, `all`), `sort`, `priceMin`, `priceMax`, `dormId`, `page`. → `{ lots, page, totalPages, total }`, 20 на страницу |
| GET | `/lots/{id}` | Один лот |
| POST | `/lots` | Создать (сессия) → 201 `Lot` |
| DELETE | `/lots/{id}` | Отменить: только автор, только активный и **без ставок** → `Lot` |
| GET | `/lots/{id}/similar` | До 4 активных лотов той же категории |
| GET | `/lots/random` | `{ id }` случайного активного (`category`, `sub` опционально), `{ id: null }` если нет |

`sort`: `ending` (по умолчанию: активные по близости окончания), `new`, `price_asc`, `price_desc`,
`title_asc`, `title_desc`, `bids`. Поиск `q` — по названию и описанию без учёта регистра.

### Создание лота — два шага
1. `POST /api/uploads` (multipart, поле `file`, JPEG/PNG/GIF/WebP до 5 МБ, тип определяется по содержимому) → `201 { url: "/uploads/<имя>" }`.
2. `POST /api/lots` `{ title, description, image: <url из шага 1>, category, subcategory, startPrice, step, hours }`.

Лимиты: название ≤300, описание ≤5000, цена и шаг 1…100 000 000, `hours` от 1 минуты (0.0167) до 720 (30 дней),
подкатегория должна принадлежать категории. Локальные `blob:`-ссылки не принимаются.

## Ставки
| Метод | Путь | Описание |
|---|---|---|
| GET | `/lots/{id}/bids` | `[{ id, lotId, userId, username, amount, createdAt }]`, новые первыми |
| POST | `/lots/{id}/bids` | `{ amount }` → 201 `{ bid, lot, extended }` (лот уже обновлён; `extended: true` — сработал Anti-snipe) |

Правила проверяет сервер (транзакция с блокировкой лота): не свой лот (403 `own_lot`), аукцион не закрыт
(409 `lot_closed`), не лидер (409 `already_leading`), `amount ≥ minBid` (400 `bid_too_low`).
Клиентские проверки оставьте только для удобства — доверять им нельзя.

### Anti-snipe
Ставка, принятая меньше чем за `ANTI_SNIPE_WINDOW_SEC` (по умолчанию 120 с) до конца аукциона, переносит конец на
«момент ставки + `ANTI_SNIPE_EXTEND_SEC`» (по умолчанию 120 с). Срок только растёт и никогда не сокращается;
продлений может быть сколько угодно. `ANTI_SNIPE_WINDOW_SEC=0` выключает защиту. Время сверяется по часам БД
внутри транзакции ставки, поэтому ставка не может «проскочить» после конца аукциона.
Настройки сервер сообщает в WebSocket-сообщении `hello` (`antiSnipe`).

## WebSocket (реалтайм)
`GET /api/ws` → апгрейд до WebSocket (RFC 6455), текстовые JSON-сообщения. Сессия берётся из cookie `session`
при подключении: без неё можно следить за лотами, но не ставить. Браузер откроет сокет сам:
`new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/api/ws')`.
Запросы с чужого `Origin` отклоняются (403); дополнительные источники — переменная `WS_ALLOWED_ORIGINS`.
Лимиты: сообщение ≤ 4 КБ, ≤ 50 подписок на соединение, ≤ 100 сообщений за 10 с (иначе `rate_limited`,
при многократном превышении соединение закрывается кодом 1008). Сервер шлёт ping каждые 25 с (браузер отвечает
сам); молчащее соединение закрывается через 60 с. Реалтайм работает в одном экземпляре бэкенда.

**Клиент → сервер**
| Сообщение | Ответ |
|---|---|
| `{ "type": "subscribe", "lotId": 12, "reqId": "1" }` | `snapshot` (текущее состояние), затем события по лоту |
| `{ "type": "unsubscribe", "lotId": 12 }` | `unsubscribed` |
| `{ "type": "bid", "lotId": 12, "amount": 1500, "reqId": "2" }` | `bid_ok` или `error` (правила и коды те же, что у REST) |
| `{ "type": "ping" }` | `pong` с серверным временем |

`reqId` — необязательная метка до 64 символов, возвращается в ответе. Ставить можно и через REST — оба пути
равноценны, события рассылаются в любом случае.

**Сервер → клиент** (`now` — серверное время, мс Unix)
| `type` | Поля | Когда |
|---|---|---|
| `hello` | `now, userId\|null, antiSnipe { enabled, windowMs, extendMs }` | сразу после подключения |
| `snapshot` | `lotId, lot, now, reqId?` | ответ на `subscribe` |
| `bid` | `lotId, bid, lot, extended, now` | новая ставка на лот (всем подписчикам, включая ставившего) |
| `bid_ok` | `lotId, bid, lot?, extended, now, reqId?` | подтверждение ставки, сделанной через WS (приходит после `bid`) |
| `lot_finished` | `lotId, lot, now` | аукцион закончился по таймеру сервера |
| `lot_cancelled` | `lotId, lot, now` | автор отменил лот |
| `unsubscribed`, `pong` | `lotId` / `now` | ответы |
| `error` | `code, message, reqId?, lotId?` | отказ; коды REST (`bid_too_low`, `lot_closed`, …) и служебные: `bad_json`, `bad_request`, `validation`, `unauthorized`, `unknown_type`, `not_found`, `too_many_subscriptions`, `rate_limited`, `internal` |

`lot` в событиях — публичное состояние `{ id, status, currentPrice, minBid, bidsCount, leaderId, endsAt }`,
одинаковое для всех. Полей, зависящих от зрителя (`myMaxBid`, `contact`), там нет: получив `lot_finished`,
перезапросите `GET /api/lots/{id}` (там появятся контакты победителя и продавца).

Гарантии: события одного лота приходят в порядке фиксации в БД, `snapshot` не может обогнать или потерять
событие. Статусы `finished` и `cancelled` терминальны. При обрыве соединения переподключитесь и снова
отправьте `subscribe` — снимок восстановит актуальное состояние (пропущенные события заново не присылаются).

## Что менять во фронтенде
`store.js` сейчас синхронный и содержит бизнес-логику. При интеграции: цены, лидера, число ставок, статус,
фильтры, сортировку и пагинацию брать с сервера (поля `Lot`), а не считать по моковым массивам;
`placeBid`/`cancelLot`/`register`/`login` превратить в вызовы `src/api.js` с показом `error`;
сессию определять через `GET /auth/me` при старте (не хранить `currentUserId` в localStorage);
в форме создания лота загружать файл через `uploadImage`, а не `URL.createObjectURL`.
