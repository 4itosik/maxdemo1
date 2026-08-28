// Package maxapi — структуры и HTTP-клиент Max Bot API, порождённые
// oapi-codegen напрямую из openapi.MaxBotApi.yaml.
//
// Второй, параллельный вариант кодогенерации: рядом лежит gen/quicktype, собранный
// quicktype через промежуточную JSON Schema. Оба порождаются из одного
// контракта; какой оставить — решается сравнением, см. `make compare-gen`.
package maxapi
