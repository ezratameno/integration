docker-run:
	docker run -d -p 3000:3000 -p 2222:22 --name gitea gitea/gitea:1.21.11

docker-remove:
	docker container rm -f gitea