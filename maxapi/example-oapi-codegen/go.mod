module example-oapi-codegen

go 1.24.0

require maxapi-oapi-codegen v0.0.0

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/oapi-codegen/runtime v1.7.0 // indirect
)

replace maxapi-oapi-codegen => ../gen/oapi-codegen
