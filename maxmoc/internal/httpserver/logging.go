package httpserver

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"maxmock/internal/logx"
)

// wsPath — единственный служебный маршрут, который нельзя оборачивать:
// websocket.Accept забирает соединение через http.Hijacker, а строка «запрос
// длился сорок минут» ничего не объясняет. Подключение и отключение UI
// печатает сам обработчик websocket.
const wsPath = "/mock/ws"

// logged регистрирует маршруты в мультиплексоре, оборачивая каждый обработчик
// строкой лога. Подставляется вместо *http.ServeMux там, где раскладку задаёт
// другой пакет (controlapi.Routes).
type logged struct{ mux *http.ServeMux }

func (l logged) Handle(pattern string, h http.Handler) {
	l.mux.Handle(pattern, requestLog(h))
}

func (l logged) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	l.mux.Handle(pattern, requestLog(http.HandlerFunc(h)))
}

// bodies — какие тела маршрута имеет смысл печатать.
type bodies struct{ req, resp bool }

// policyFor выбирает уровень строки и печатаемые тела по пути.
//
// Разделение здесь, а не в месте регистрации маршрутов: сама раскладка адресов
// уже описана в New, и дублировать её списком обёрток значило бы заводить
// второй источник правды, который разойдётся на первом же новом эндпоинте.
func policyFor(path string) (slog.Level, bodies) {
	switch {
	// Статика, страницы и проба живости. На каждый заход в UI их десяток, и
	// ни один не говорит о работе мока — им место на debug.
	case strings.HasPrefix(path, "/mock/static/"), strings.HasPrefix(path, "/mock/chat/"),
		path == "/mock", path == "/mock/", path == "/healthz":
		return slog.LevelDebug, bodies{}
	// Приём файла: тело запроса — сам файл, до 256 МБ. Ответ маленький и
	// полезный: в нём токен вложения.
	case strings.HasPrefix(path, "/mock/upload/"), strings.HasSuffix(path, "/files"):
		return slog.LevelInfo, bodies{resp: true}
	// Отдача файла: то же самое наоборот.
	case strings.HasPrefix(path, "/mock/files/"):
		return slog.LevelInfo, bodies{}
	default:
		return slog.LevelInfo, bodies{req: true, resp: true}
	}
}

// recorder перехватывает ответ служебной поверхности: без него строка лога не
// знает ни статуса, ни размера.
type recorder struct {
	http.ResponseWriter
	status  int
	size    int64
	body    bytes.Buffer
	capture bool
}

func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.size += int64(n)
	if r.capture {
		// Буферизуем на байт больше предела: тело ровно в предел иначе
		// неотличимо от обрезанного, и на нём появилась бы ложная пометка.
		if room := logx.Limit() + 1 - r.body.Len(); room > 0 && n > 0 {
			r.body.Write(b[:min(n, room)])
		}
	}
	return n, err
}

// Unwrap открывает http.ResponseController дорогу к настоящему ответу: без
// него http.ServeContent теряет Flush, и перехват сломал бы отдачу файлов.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// requestLog печатает строку на каждый запрос служебной поверхности: control
// API, приём и отдачу файлов, страницы и статику.
//
// Фасад Bot API через неё не проходит — он логирует себя сам, зная бота и
// диалог, которых по одному пути не восстановить.
func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == wsPath {
			next.ServeHTTP(w, r)
			return
		}
		level, want := policyFor(r.URL.Path)
		start := time.Now()

		var reqBody []byte
		if want.req && logx.Limit() > 0 {
			// Читаем ровно столько, сколько попадёт в лог, и возвращаем
			// прочитанное обратно в поток: обработчик должен получить тело
			// целиком, включая хвост за пределом.
			reqBody, _ = io.ReadAll(io.LimitReader(r.Body, int64(logx.Limit())+1))
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(reqBody), r.Body))
		}

		rec := &recorder{ResponseWriter: w, capture: want.resp && logx.Limit() > 0}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			// Обработчик не написал ничего вовсе — net/http отдаст 200.
			rec.status = http.StatusOK
		}

		// Отказ виден всегда, даже на маршруте, отведённом под debug:
		// 404 на статике — это сломанная сборка, а не фоновый шум.
		if fail := logx.LevelForStatus(rec.status); fail > slog.LevelInfo {
			level = fail
		}
		slog.LogAttrs(r.Context(), level, "служебный запрос",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			logx.Query(r.URL.RawQuery),
			slog.Int("status", rec.status),
			slog.Int64("ms", time.Since(start).Milliseconds()),
			logx.Int64("байт", rec.size),
			// Полные размеры берутся не из прочитанного: тела здесь
			// намеренно недочитаны до предела лога.
			logx.BodyOf("req", reqBody, int(r.ContentLength)),
			logx.BodyOf("resp", rec.body.Bytes(), int(rec.size)),
		)
	})
}
