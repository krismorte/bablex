.PHONY: local local-down test build deploy

local: 
	docker-compose up --build

update:
	docker-compose down
	docker-compose build --no-cache
	docker-compose up

local-down: 
	docker-compose down

test: 
	./test.sh

build: 
	./build.sh

deploy:	
	./deploy.sh

test-backend:
	cd backend && go test ./...

test-frontend:
	cd frontend && npm test

invalidate:
	./invalidate-cloudfront.sh