// Package webhook принимает события Max по HTTP и передаёт их обработчику.
package webhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"maxbotdemo/internal/jsonlog"
	"maxbotdemo/internal/maxapi"
)

// SecretHeader — заголовок, в котором Max присылает секрет подписки.
const SecretHeader = "X-Max-Bot-Api-Secret"

// maxConcurrentHandlers ограничивает число одновременно работающих обработчиков.
const maxConcurrentHandlers = 32

// handlerTimeout ограничивает время обработки одного события.
const handlerTimeout = 30 * time.Second

// maxBodyBytes ограничивает размер тела события. Лимит нужен потому, что тело
// читается в память целиком: иначе один большой запрос стоил бы боту памяти.
const maxBodyBytes = 1 << 20

// Handler обрабатывает одно событие.
type Handler func(ctx context.Context, u maxapi.Update)

// Server — http.Handler, принимающий webhook-запросы от Max.
type Server struct {
	secret  string
	handle  Handler
	log     *slog.Logger
	slots   chan struct{}
	running sync.WaitGroup
}

// New создаёт сервер. Пустой secret отключает проверку заголовка.
func New(secret string, handle Handler, log *slog.Logger) *Server {
	return &Server{
		secret: secret,
		handle: handle,
		log:    log,
		slots:  make(chan struct{}, maxConcurrentHandlers),
	}
}

// ServeHTTP проверяет запрос, отвечает 200 OK и запускает обработку в фоне.
//
// Ответ отдаётся до обработки намеренно: Max ждёт быстрый 200, иначе повторит
// доставку события.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "разрешён только POST", http.StatusMethodNotAllowed)
		return
	}

	if !s.secretOK(r) {
		s.log.Warn("запрос с неверным секретом отклонён", "remote_addr", r.RemoteAddr)
		http.Error(w, "неверный секрет", http.StatusUnauthorized)
		return
	}

	// Тело читается целиком, а не разбирается из потока: в лог оно должно
	// попасть в точности таким, каким его прислал MAX.
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.log.Warn("не удалось прочитать тело запроса", "error", err)
		http.Error(w, "тело запроса не прочитано", http.StatusBadRequest)
		return
	}

	var u maxapi.Update
	if err := json.Unmarshal(raw, &u); err != nil {
		s.log.Warn("не удалось разобрать событие", "error", err, "payload", jsonlog.Raw(raw))
		http.Error(w, "тело запроса не является объектом Update", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	s.log.Info("получено событие", "update_type", u.UpdateType, "payload", jsonlog.Raw(raw))

	s.running.Add(1)
	go func() {
		defer s.running.Done()

		// Контекст запроса отменяется сразу после ответа, поэтому обработчику
		// нужен свой.
		ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
		defer cancel()

		s.slots <- struct{}{}
		defer func() { <-s.slots }()

		s.handle(ctx, u)
	}()
}

// secretOK сравнивает заголовок с настроенным секретом за постоянное время.
func (s *Server) secretOK(r *http.Request) bool {
	if s.secret == "" {
		return true
	}
	got := r.Header.Get(SecretHeader)
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.secret)) == 1
}

// Wait ждёт завершения запущенных обработчиков. Вызывается при остановке
// приложения, после http.Server.Shutdown.
func (s *Server) Wait() {
	s.running.Wait()
}
