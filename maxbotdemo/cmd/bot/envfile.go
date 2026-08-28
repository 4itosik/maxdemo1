package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// parseEnvFile читает файл вида `ИМЯ=значение` — тот же формат, что понимает
// `set -a && . ./.env && set +a` из README: строки-комментарии с `#`, пустые
// строки, необязательный префикс `export`, кавычки вокруг значения.
//
// Комментарий в конце строки не поддерживается намеренно: `#` встречается
// внутри токенов и секретов, а молча обрезанное значение ищут долго — лучше
// пусть `#` останется частью значения, как и задумано.
func parseEnvFile(r io.Reader) (map[string]string, error) {
	vars := make(map[string]string)
	sc := bufio.NewScanner(r)

	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("строка %d: ожидалось ИМЯ=значение, получено %q", n, line)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("строка %d: пустое имя переменной", n)
		}
		vars[name] = unquote(strings.TrimSpace(value))
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("чтение файла переменных: %w", err)
	}
	return vars, nil
}

// unquote снимает парные кавычки вокруг значения — их же снимает shell,
// когда файл подключают через `.`.
func unquote(v string) string {
	if len(v) < 2 {
		return v
	}
	q := v[0]
	if (q == '\'' || q == '"') && v[len(v)-1] == q {
		return v[1 : len(v)-1]
	}
	return v
}

// readEnvFile читает переменные из файла по пути path.
func readEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("файл переменных окружения: %w", err)
	}
	defer func() { _ = f.Close() }()

	vars, err := parseEnvFile(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return vars, nil
}

// envFirst накладывает файл на окружение процесса: значение из окружения
// сильнее. Так `WEBHOOK_URL='https://новый-туннель…' ./bot .bot.env` перебивает
// адрес из файла — приём, на котором построен раздел про туннель в README.
func envFirst(getenv func(string) string, vars map[string]string) func(string) string {
	return func(key string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return vars[key]
	}
}

// environment собирает источник переменных для loadConfig.
//
// Без аргументов это окружение процесса — так бот запускали всегда. Один
// аргумент — путь к файлу переменных (`./bot .bot.env`): его значения
// подставляются там, где в окружении пусто.
func environment(args []string) (func(string) string, error) {
	switch len(args) {
	case 0:
		return os.Getenv, nil
	case 1:
		vars, err := readEnvFile(args[0])
		if err != nil {
			return nil, err
		}
		return envFirst(os.Getenv, vars), nil
	default:
		return nil, fmt.Errorf("лишние аргументы %v: ожидался один путь к файлу переменных окружения", args[1:])
	}
}
