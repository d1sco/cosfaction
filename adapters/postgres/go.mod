module github.com/d1sco/cosfaction/adapters/postgres

go 1.22.2

toolchain go1.24.2

require (
	github.com/d1sco/cosfaction v0.1.1
	github.com/jackc/pgx/v5 v5.6.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	golang.org/x/crypto v0.17.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace github.com/d1sco/cosfaction => ../..
