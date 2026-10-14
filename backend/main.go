package main

import (
	"fmt"
	"net/http"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		fmt.Println("error:", err)
	}
}

func main() {
	http.HandleFunc("/health", healthHandler)

	fmt.Println("Запуск HTTP сервера...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Println("В ходе работы HTTP сервера произошла ошибка:", err.Error())
	}
}
