package handler

import "net/http"

// WS — WebSocket-эндпоинт реалтайма (GET /api/ws, протокол описан в docs/API.md).
// Сессия берётся из cookie при рукопожатии; без неё можно только следить за лотами.
// Браузерный WebSocket не умеет слать заголовки, поэтому Authorization здесь
// нужен лишь не-браузерным клиентам.
func (h *Handler) WS(w http.ResponseWriter, r *http.Request) {
	h.hub.ServeWS(w, r, h.userID(r))
}
