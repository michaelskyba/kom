module github.com/veilm/hinata/hnt-apply

go 1.21

require (
	github.com/spf13/cobra v1.9.1
	github.com/veilm/hinata/llm-pack v0.0.0-20250713041408-6b1fa4d93e23
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
)

replace github.com/veilm/hinata/llm-pack => ../llm-pack
