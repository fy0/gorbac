module examples/test-project-expr

go 1.24.0

toolchain go1.24.6

replace github.com/fy0/gorbac/v3 => ../../..

require (
	github.com/google/cel-go v0.26.1
	github.com/fy0/gorbac/v3 v3.0.0-00010101000000-000000000000
)

require (
	cel.dev/expr v0.24.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.0 // indirect
	github.com/stoewer/go-strcase v1.2.0 // indirect
	golang.org/x/exp v0.0.0-20230515195305-f3d0a9c9a5cc // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260128011058-8636f8732409 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260122232226-8e98ce8d340d // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

