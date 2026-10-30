.PHONY: site serve covers api test

site:        ## собрать сайт в dist/
	go run ./cmd/site

serve:       ## собрать и открыть на http://localhost:8000
	go run ./cmd/site -serve :8000

covers:      ## докачать недостающие обложки
	go run ./cmd/covers

api:         ## запустить бэкенд аккаунтов на :8080
	go run ./cmd/api

test:
	go vet ./... && go test ./...
