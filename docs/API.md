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
* `currentPrice` — лидирующая ставка или `startPrice`; `minBid = currentPrice + step`.
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
| POST | `/lots/{id}/bids` | `{ amount }` → 201 `{ bid, lot }` (лот уже обновлён) |

Правила проверяет сервер (транзакция с блокировкой лота): не свой лот (403 `own_lot`), аукцион не закрыт
(409 `lot_closed`), не лидер (409 `already_leading`), `amount ≥ minBid` (400 `bid_too_low`).
Клиентские проверки оставьте только для удобства — доверять им нельзя.

## Что менять во фронтенде
`store.js` сейчас синхронный и содержит бизнес-логику. При интеграции: цены, лидера, число ставок, статус,
фильтры, сортировку и пагинацию брать с сервера (поля `Lot`), а не считать по моковым массивам;
`placeBid`/`cancelLot`/`register`/`login` превратить в вызовы `src/api.js` с показом `error`;
сессию определять через `GET /auth/me` при старте (не хранить `currentUserId` в localStorage);
в форме создания лота загружать файл через `uploadImage`, а не `URL.createObjectURL`.
