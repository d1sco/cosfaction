module github.com/d1sco/cosfaction/adapters/nats

go 1.22.2

toolchain go1.24.2

require (
	github.com/d1sco/cosfaction v0.1.1
	github.com/nats-io/nats.go v1.36.0
)

require (
	github.com/klauspost/compress v1.17.2 // indirect
	github.com/nats-io/nkeys v0.4.7 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.18.0 // indirect
	golang.org/x/sys v0.16.0 // indirect
)

replace github.com/cosfaction/cosfaction => ../..
