docker-run:
	docker run -d -p 3000:3000 -p 2222:22  -e GITEA__security__INSTALL_LOCK=true --name gitea gitea/gitea:1.21.7

docker-remove:
	docker container rm -f gitea

docker-restart: docker-remove docker-run
tidy:
	go mod tidy