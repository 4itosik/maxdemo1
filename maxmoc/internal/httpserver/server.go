// Package httpserver собирает все поверхности мока в один обработчик.
//
// Раскладка адресов: Bot API занимает корень (как настоящий
// platform-api2.max.ru), всё служебное — префикс /mock. Мультиплексор Go
// отдаёт предпочтение более конкретному образцу, поэтому "/" в конце ловит
// только то, что не разобрали служебные маршруты, и передаёт фасаду —
// включая неизвестные пути, на которые тот отвечает mock.not_implemented.
package httpserver

import (
	"io/fs"
	"net/http"

	"maxmock/internal/config"
	"maxmock/internal/controlapi"
	"maxmock/internal/core"
	"maxmock/internal/events"
	"maxmock/internal/maxfacade"
	"maxmock/internal/specs"
	"maxmock/web"
)

// New собирает обработчик всего сервиса.
func New(c *core.Core, sp *specs.Specs, bus *events.Bus, cfg *config.Config) http.Handler {
	mux := http.NewServeMux()
	// Служебные маршруты регистрируются через обёртку: она навешивает на
	// каждый обработчик строку лога, не трогая саму раскладку адресов.
	// Раскладка важна — «/» в конце ловит всё неразобранное и отдаёт фасаду,
	// который отвечает на такое честным mock.not_implemented.
	service := logged{mux}

	controlapi.New(c, bus).Routes(service)

	service.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	static, err := fs.Sub(web.FS, "static")
	if err != nil {
		panic("httpserver: встроенная статика недоступна: " + err.Error())
	}
	service.Handle("GET /mock/static/", http.StripPrefix("/mock/static/", http.FileServerFS(static)))

	page := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			body, err := web.FS.ReadFile("static/" + name)
			if err != nil {
				http.Error(w, "страница не найдена", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(body)
		}
	}
	// Админка ботов и веб-чат тестировщика.
	service.HandleFunc("GET /mock", page("admin.html"))
	service.HandleFunc("GET /mock/{$}", page("admin.html"))
	service.HandleFunc("GET /mock/chat/{botId}", page("chat.html"))

	mux.Handle("/", maxfacade.New(c, sp, bus, cfg))
	return mux
}
