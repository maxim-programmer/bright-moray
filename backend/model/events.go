package model

// Типы событий журнала.
const (
	EvUserRegistered = "user.registered"
	EvUserLogin      = "user.login"
	EvUserBlocked    = "user.blocked"
	EvUserUnblocked  = "user.unblocked"

	EvLotCreated     = "lot.created"
	EvLotUpdated     = "lot.updated"
	EvLotApproved    = "lot.approved"
	EvLotRejected    = "lot.rejected"
	EvLotUnpublished = "lot.unpublished"
	EvLotDeleted     = "lot.deleted"
	EvLotCancelled   = "lot.cancelled" // отмена автором

	EvBidPlaced  = "bid.placed"
	EvBidRemoved = "bid.removed"

	EvAuctionStarted   = "auction.started"
	EvAuctionFinished  = "auction.finished"
	EvAuctionForced    = "auction.force_finished"
	EvAuctionCancelled = "auction.cancelled"        // отмена администратором
	EvAuctionAnnulled  = "auction.annulled"         // результат аннулирован при разборе спора
	EvDisputeResolved  = "auction.dispute_resolved" // спор рассмотрен без изменения результата

	EvSettingsUpdated = "settings.updated"
	EvCategoryCreated = "category.created"
	EvCategoryUpdated = "category.updated"
	EvCategoryDeleted = "category.deleted"
	EvSubCreated      = "subcategory.created"
	EvSubUpdated      = "subcategory.updated"
	EvSubDeleted      = "subcategory.deleted"
	EvDormCreated     = "dorm.created"
	EvDormUpdated     = "dorm.updated"
	EvDormDeleted     = "dorm.deleted"

	EvAdminCLICreated  = "admin.cli_created"  // учётная запись создана командой make admin-create
	EvAdminCLIPromoted = "admin.cli_promoted" // существующий пользователь повышен до администратора
	EvAdminCLIDemoted  = "admin.cli_demoted"  // роль администратора снята
	EvAdminCLIDeleted  = "admin.cli_deleted"  // учётная запись, созданная командой, удалена
)

// EventLabels — подписи типов событий для интерфейса (порядок — как в фильтре).
var EventLabels = []struct{ Type, Label string }{
	{EvUserRegistered, "Регистрация пользователя"},
	{EvUserLogin, "Вход в аккаунт"},
	{EvUserBlocked, "Пользователь заблокирован"},
	{EvUserUnblocked, "Пользователь разблокирован"},
	{EvLotCreated, "Лот создан"},
	{EvLotUpdated, "Лот изменён"},
	{EvLotApproved, "Лот одобрен"},
	{EvLotRejected, "Лот отклонён"},
	{EvLotUnpublished, "Лот снят с публикации"},
	{EvLotDeleted, "Лот удалён"},
	{EvLotCancelled, "Лот отменён автором"},
	{EvBidPlaced, "Ставка"},
	{EvBidRemoved, "Ставка удалена"},
	{EvAuctionStarted, "Аукцион начался"},
	{EvAuctionFinished, "Аукцион завершён"},
	{EvAuctionForced, "Аукцион завершён принудительно"},
	{EvAuctionCancelled, "Аукцион отменён администратором"},
	{EvAuctionAnnulled, "Результат аукциона аннулирован"},
	{EvDisputeResolved, "Спор рассмотрен без изменений"},
	{EvSettingsUpdated, "Изменены настройки"},
	{EvCategoryCreated, "Категория добавлена"},
	{EvCategoryUpdated, "Категория изменена"},
	{EvCategoryDeleted, "Категория удалена"},
	{EvSubCreated, "Подкатегория добавлена"},
	{EvSubUpdated, "Подкатегория изменена"},
	{EvSubDeleted, "Подкатегория удалена"},
	{EvDormCreated, "Общежитие добавлено"},
	{EvDormUpdated, "Общежитие изменено"},
	{EvDormDeleted, "Общежитие удалено"},
	{EvAdminCLICreated, "Администратор создан (терминал)"},
	{EvAdminCLIPromoted, "Пользователь назначен администратором (терминал)"},
	{EvAdminCLIDemoted, "Роль администратора снята (терминал)"},
	{EvAdminCLIDeleted, "Администратор удалён (терминал)"},
}

// EventLabel возвращает подпись типа события; неизвестный тип возвращается как есть.
func EventLabel(t string) string {
	for _, e := range EventLabels {
		if e.Type == t {
			return e.Label
		}
	}
	return t
}
